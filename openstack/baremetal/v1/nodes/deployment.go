package nodes

import (
	"fmt"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/baremetal/v1/nodes"
	"time"
)

type DeploymentState string

const (
	StateBegin = "BEGIN"
	StateConfigure = "CONFIGURE"
	StateManage = "MANAGE"
	StateWaitManage = "WAIT_MANAGE"
	StateProvide = "PROVIDE"
	StateWaitProvide = "WAIT_PROVIDE"
	StateDeploy = "DEPLOY"
	StateWaitDeploy = "WAIT_DEPLOY"
	StateDone = "DONE"
)

type Deployment struct {
	NodeUUID     string
	InstanceInfo map[string]string
	Properties   map[string]string
	Error        error

	// Internal
	currentState DeploymentState
	status       chan string
}

// Prepares and deploys an Ironic baremetal node by driving the Ironic state machine through the needed steps, as per
// the configuration specified in the *Deployment struct. May be  run as a goroutine, pass in a channel to receive
// updates on the deployment's progress.
func Deploy(client *gophercloud.ServiceClient, deployment *Deployment, status chan string) error {
	deployment.currentState = StateBegin

	if status != nil {
		status <- StateBegin
		deployment.status = status
		defer close(deployment.status)
	}

	return nextState(client, deployment)
}

// Configures a node per the settings specified in the *Deployment struct.
func configure(client *gophercloud.ServiceClient, deployment *Deployment) error {
	_, err := nodes.Update(client, deployment.NodeUUID, nodes.UpdateOpts{
		nodes.UpdateOperation{
			Op:    nodes.AddOp,
			Path:  "/instance_info",
			Value: deployment.InstanceInfo,
		},
		nodes.UpdateOperation{
			Op:    nodes.AddOp,
			Path:  "/properties",
			Value: deployment.Properties,
		},
	}).Extract()

	if err != nil {
		deployment.Error = err
	}

	return nextState(client, deployment)
}

// Sets a node to Manage
func manage(client *gophercloud.ServiceClient, deployment *Deployment) error {
	err := nodes.ChangeProvisionState(client, deployment.NodeUUID, nodes.ProvisionStateOpts{
		Target: "manage",
	}).ExtractErr()

	if err != nil {
		deployment.Error = err
	}

	return nextState(client, deployment)
}

// Waits for a node to be manageable, or for an error to occur
func waitManage(client *gophercloud.ServiceClient, deployment *Deployment) error {
	var err error

	for node, err := nodes.Get(client, deployment.NodeUUID).Extract(); err == nil; {
		if node.ProvisionState == nodes.Manageable {
			break
		} else if node.ProvisionState == nodes.Verifying {
			time.Sleep(500000)
		} else {
			deployment.Error = fmt.Errorf("Manage failed, node's current state is: %+v", node.ProvisionState)
		}
	}

	if err != nil {
		deployment.Error = err
	}

	return nextState(client, deployment)
}

func provide(client *gophercloud.ServiceClient, deployment *Deployment) error {
	err := nodes.ChangeProvisionState(client, deployment.NodeUUID, nodes.ProvisionStateOpts{
		Target: "provide",
	}).ExtractErr()

	if err != nil {
		return fmt.Errorf("Could not change node to 'provide'.")
	}

	return nextState(client, deployment)
}

func waitProvide(client *gophercloud.ServiceClient, deployment *Deployment) error {

	for node, err := nodes.Get(client, deployment.NodeUUID).Extract(); err == nil; {
		if node.ProvisionState == nodes.Available {
			break
		} else if node.ProvisionState == nodes.Cleaning {
			time.Sleep(500000)
		} else {
			return fmt.Errorf("Provide failed, node's current state is: %+v", node.ProvisionState)
		}
	}

	return nextState(client, deployment)
}

func deploy(client *gophercloud.ServiceClient, deployment *Deployment) error {

	err := nodes.ChangeProvisionState(client, deployment.NodeUUID, nodes.ProvisionStateOpts{
		Target: "active",
	}).ExtractErr()

	if err != nil {
		return fmt.Errorf("Could not change node to 'active'.")
	}

	return nextState(client, deployment)
}

func waitDeploy(client *gophercloud.ServiceClient, deployment *Deployment) error {
	var err error

	for node, err := nodes.Get(client, deployment.NodeUUID).Extract(); err == nil; {
		if node.ProvisionState == nodes.Active {
			break
		} else if node.ProvisionState == nodes.DeployWait {
			time.Sleep(500000)
		} else {
			deployment.Error = fmt.Errorf("Deploy failed, node's current state is: %+v", node.ProvisionState)
		}
	}

	if err != nil {
		deployment.Error = err
	}

	return nextState(client, deployment)
}

// Great success
func done(_ *gophercloud.ServiceClient, deployment *Deployment) error {
	return deployment.Error
}

func nextState(client *gophercloud.ServiceClient, deployment *Deployment) error {
	var nextState func(*gophercloud.ServiceClient, *Deployment) error

	if deployment.Error != nil {
		if deployment.status != nil {
			deployment.status <- "ERROR"
		}
		return done(client, deployment)
	}

	switch state := deployment.currentState; state {
	case StateBegin:
		nextState = configure
		deployment.currentState = StateConfigure
	case StateConfigure:
		nextState = manage
		deployment.currentState = StateManage
	case StateManage:
		nextState = waitManage
		deployment.currentState = StateWaitManage
	case StateWaitManage:
		nextState = provide
		deployment.currentState = StateProvide
	case StateProvide:
		nextState = waitProvide
		deployment.currentState = StateWaitProvide
	case StateWaitProvide:
		nextState = deploy
		deployment.currentState = StateDeploy
	case StateDeploy:
		deployment.currentState = StateWaitDeploy
		nextState = waitDeploy
	case StateWaitDeploy:
		deployment.currentState = StateDone
		nextState = done
	default:
		return fmt.Errorf("Unknown state")
	}

	// Go to next state
	if deployment.status != nil {
		deployment.status <- string(deployment.currentState)
	}

	return nextState(client, deployment)
}


