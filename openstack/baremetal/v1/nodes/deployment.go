package nodes

import (
	"fmt"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/baremetal/v1/nodes"
)

// DeploymentState tracks the current state of an Ironic node deployment.
type DeploymentState string

const (
	StateBegin              DeploymentState = "BEGIN"
	StateBeginPercent       int             = 0
	StateConfigure          DeploymentState = "CONFIGURE"
	StateConfigurePercent   int             = 10
	StateManage             DeploymentState = "MANAGE"
	StateManagePercent      int             = 15
	StateWaitManage         DeploymentState = "WAIT_MANAGE"
	StateWaitManagePercent  int             = 20
	StateProvide            DeploymentState = "PROVIDE"
	StateProvidePercent     int             = 25
	StateWaitProvide        DeploymentState = "WAIT_PROVIDE"
	StateWaitProvidePercent int             = 30
	StateDeploy             DeploymentState = "DEPLOY"
	StateDeployPercent      int             = 35
	StateWaitDeploy         DeploymentState = "WAIT_DEPLOY"
	StateWaitDeployPercent  int             = 95
	StateDone               DeploymentState = "DONE"
	StateDonePercent        int             = 100
)

type Deployment struct {
	NodeUUID    string
	UpdateOpts  nodes.UpdateOpts
	ConfigDrive ConfigDriveBuilder
	Error       error
	Timeout     int64
	Delay       int64

	client         *gophercloud.ServiceClient
	currentState   DeploymentState
	currentPercent int
	status         chan<- int
}

// Prepares and deploys an Ironic baremetal node by driving the Ironic state machine through the needed steps, as per
// the configuration specified in the *Deployment struct. May be run as a goroutine, pass in a channel to receive
// updates on the deployment's percentage.
func Deploy(client *gophercloud.ServiceClient, deployment *Deployment, percent chan<- int) error {
	deployment.currentState = StateBegin
	deployment.client = client

	if percent != nil {
		deployment.status = percent
		deployment.status <- StateBeginPercent
	} else {
		deployment.status = make(chan<- int)
	}
	defer close(deployment.status)

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
			deployment.Error = fmt.Errorf("manage failed: %+v current state is: %+v", node.Name, node.ProvisionState)
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
	for {
		node, err := nodes.Get(deployment.client, deployment.NodeUUID).Extract()
		if err != nil {
			deployment.Error = err
			break
		}

		if node.ProvisionState == nodes.Available {
			break
		} else if node.ProvisionState == nodes.Cleaning {
			time.Sleep(5 * time.Second)
		} else {
			return fmt.Errorf("provide failed, %+v current state is: %+v", node.Name, node.ProvisionState)
		}
	}

	return deployment.nextState()
}

func (deployment *Deployment) deploy() error {
	configDrive, err := deployment.ConfigDrive.ToConfigDrive()
	if err != nil {
		deployment.Error = err
		return deployment.nextState()
	}

	err = nodes.ChangeProvisionState(deployment.client, deployment.NodeUUID, nodes.ProvisionStateOpts{
		Target:      "active",
		ConfigDrive: string(configDrive),
	}).ExtractErr()

	deployment.Error = err
	return deployment.nextState()
}

func (deployment *Deployment) waitDeploy() error {
	for {
		node, err := nodes.Get(deployment.client, deployment.NodeUUID).Extract()
		if err != nil {
			deployment.Error = err
			break
		}

		if node.ProvisionState == nodes.Active {
			break
		} else if node.ProvisionState == nodes.DeployWait || node.ProvisionState == nodes.Deploying {
			if deployment.currentPercent < StateWaitDeployPercent {
				deployment.currentPercent = deployment.currentPercent + 2
				deployment.status <- deployment.currentPercent
			}
			time.Sleep(5 * time.Second)
		} else {
			deployment.Error = fmt.Errorf("deploy failed: %+v current state is: %+v", node.Name, node.ProvisionState)
			break
		}
	}

	return deployment.nextState()
}

// Great success, or utter failure, either way we're done and we should finally return.
func (deployment *Deployment) done() error {
	deployment.status <- StateDonePercent
	return deployment.Error
}

// Transitions the state machine through the various states to drive Ironic deploying a node
func (deployment *Deployment) nextState() error {
	var nextState func() error

	if deployment.Error != nil {
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
		deployment.currentPercent = StateConfigurePercent
		nextState = deployment.waitManage
		deployment.currentState = StateWaitManage
	case StateWaitManage:
		deployment.currentPercent = StateManagePercent
		nextState = deployment.provide
		deployment.currentState = StateProvide
	case StateProvide:
		deployment.currentPercent = StateWaitManagePercent
		nextState = deployment.waitProvide
		deployment.currentState = StateWaitProvide
	case StateWaitProvide:
		deployment.currentPercent = StateProvidePercent
		nextState = deployment.deploy
		deployment.currentState = StateDeploy
	case StateDeploy:
		deployment.currentPercent = StateWaitProvidePercent
		deployment.currentState = StateWaitDeploy
		nextState = deployment.waitDeploy
	case StateWaitDeploy:
		deployment.currentPercent = StateDeployPercent
		deployment.currentState = StateDone
		nextState = deployment.done
	default:
		return fmt.Errorf("unknown state")
	}

	// Update percentage
	deployment.status <- deployment.currentPercent

	// Go to next state
	return nextState()
}
