package nodes

import (
	"fmt"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/baremetal/v1/nodes"
)

// DeploymentState tracks the current state of an Ironic node deployment.
type DeploymentState struct {
	Name       string
	Percentage int
}

var (
	StateBegin       = DeploymentState{"BEGIN", 0}
	StateConfigure   = DeploymentState{"CONFIGURE", 5}
	StateManage      = DeploymentState{"MANAGE", 15}
	StateWaitManage  = DeploymentState{"WAIT_MANAGE", 20}
	StateProvide     = DeploymentState{"PROVIDE", 30}
	StateWaitProvide = DeploymentState{"WAIT_PROVIDE", 35}
	StateDeploy      = DeploymentState{"DEPLOY", 40}
	StateWaitDeploy  = DeploymentState{"WAIT_DEPLOY", 50}
	StateDone        = DeploymentState{"DONE", 100}
	StateError       = DeploymentState{"ERROR", 100}
)

type Deployment struct {
	NodeUUID     string
	UpdateOpts 	 nodes.UpdateOpts
	ConfigDrive	 ConfigDriveBuilder
	Error        error
	Timeout      int64
	Delay        int64

	// Internal
	client       *gophercloud.ServiceClient
	currentState DeploymentState
	status       chan DeploymentState
}

// Prepares and deploys an Ironic baremetal node by driving the Ironic state machine through the needed steps, as per
// the configuration specified in the *Deployment struct. May be run as a goroutine, pass in a channel to receive
// updates on the deployment's progress.
func Deploy(client *gophercloud.ServiceClient, deployment *Deployment, status chan DeploymentState) error {
	deployment.currentState = StateBegin
	deployment.client = client

	if status != nil {
		deployment.status = status
		deployment.status <- StateBegin
		defer close(deployment.status)
	}

	return deployment.nextState()
}

// Configures a node per the settings specified in the Deployment struct.
func (deployment *Deployment) configure() error {
	if len(deployment.UpdateOpts) != 0 {
		_, err := nodes.Update(deployment.client, deployment.NodeUUID, deployment.UpdateOpts).Extract()

		if err != nil {
			deployment.Error = err
		}
	}

	return deployment.nextState()
}

// Sets a node to Manage
func (deployment *Deployment) manage() error {
	err := nodes.ChangeProvisionState(deployment.client, deployment.NodeUUID, nodes.ProvisionStateOpts{
		Target: "manage",
	}).ExtractErr()

	if err != nil {
		deployment.Error = err
	}

	return deployment.nextState()
}

// Waits for a node to be manageable, or for an error to occur
func (deployment *Deployment) waitManage() error {
	for {
		node, err := nodes.Get(deployment.client, deployment.NodeUUID).Extract()
		if err != nil {
			deployment.Error = err
			break
		}

		if node.ProvisionState == nodes.Manageable {
			break
		} else if node.ProvisionState == nodes.Verifying {
			time.Sleep(5 * time.Second)
		} else {
			deployment.Error = fmt.Errorf("manage failed: node's current state is: %+v", node.ProvisionState)
		}
	}

	return deployment.nextState()
}

func (deployment *Deployment) provide() error {
	err := nodes.ChangeProvisionState(deployment.client, deployment.NodeUUID, nodes.ProvisionStateOpts{
		Target: "provide",
	}).ExtractErr()

	deployment.Error = err
	return deployment.nextState()
}

func (deployment *Deployment) waitProvide() error {
	var err error

	for node, err := nodes.Get(deployment.client, deployment.NodeUUID).Extract(); err == nil; {
		if node.ProvisionState == nodes.Available {
			break
		} else if node.ProvisionState == nodes.Cleaning {
			time.Sleep(5 * time.Second)
		} else {
			return fmt.Errorf("provide failed, node's current state is: %+v", node.ProvisionState)
		}
	}

	deployment.Error = err
	return deployment.nextState()
}

func (deployment *Deployment) deploy() error {
	configDrive, err := deployment.ConfigDrive.ToConfigDrive()
	if err != nil {
		deployment.Error = err
		return deployment.nextState()
	}

	err = nodes.ChangeProvisionState(deployment.client, deployment.NodeUUID, nodes.ProvisionStateOpts{
		Target: "active",
		ConfigDrive: string(configDrive),
	}).ExtractErr()

	deployment.Error = err
	return deployment.nextState()
}

func (deployment *Deployment) waitDeploy() error {
	var percentage = 50

	for {
		node, err := nodes.Get(deployment.client, deployment.NodeUUID).Extract()
		if err != nil {
			deployment.Error = err
		}

		if node.ProvisionState == nodes.Active {
			break
		} else if node.ProvisionState == nodes.DeployWait || node.ProvisionState == nodes.Deploying {
			if percentage < 99 {
				percentage++
			}

			deployment.status <- DeploymentState{
				Name: "WAIT_DEPLOY",
				Percentage: percentage,
			}
			time.Sleep(5 * time.Second)
		} else {
			deployment.Error = fmt.Errorf("deploy failed: node's current state is: %+v", node.ProvisionState)
			break
		}
	}

	return deployment.nextState()
}

// Great success, or utter failure, either way we're done and we should finally return.
func (deployment *Deployment) done() error {
	return deployment.Error
}

// Transitions the state machine through the various states to drive Ironic deploying a node
func (deployment *Deployment) nextState() error {
	var nextState func() error

	if deployment.Error != nil {
		if deployment.status != nil {
			deployment.status <- StateError
		}
		return deployment.done()
	}

	switch state := deployment.currentState; state {
	case StateBegin:
		nextState = deployment.configure
		deployment.currentState = StateConfigure
	case StateConfigure:
		nextState = deployment.manage
		deployment.currentState = StateManage
	case StateManage:
		nextState = deployment.waitManage
		deployment.currentState = StateWaitManage
	case StateWaitManage:
		nextState = deployment.provide
		deployment.currentState = StateProvide
	case StateProvide:
		nextState = deployment.waitProvide
		deployment.currentState = StateWaitProvide
	case StateWaitProvide:
		nextState = deployment.deploy
		deployment.currentState = StateDeploy
	case StateDeploy:
		deployment.currentState = StateWaitDeploy
		nextState = deployment.waitDeploy
	case StateWaitDeploy:
		deployment.currentState = StateDone
		nextState = deployment.done
	default:
		return fmt.Errorf("unknown state")
	}

	if deployment.status != nil {
		deployment.status <- deployment.currentState
	}

	// Go to next state
	return nextState()
}
