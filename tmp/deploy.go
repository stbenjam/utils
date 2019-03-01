package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"sync"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/baremetal/noauth"
	"github.com/gophercloud/gophercloud/openstack/baremetal/v1/nodes"
	"github.com/gophercloud/gophercloud/openstack/baremetal/v1/ports"
	deploy "github.com/gophercloud/utils/openstack/baremetal/v1/nodes"
	"github.com/gosuri/uiprogress"
)

const (
	IronicEndpoint     = "http://localhost:6385/v1/"
	IronicMicroversion = "1.46"
	IgnitionConfig     = "ocp/master.ign"
)

func main() {
	fmt.Println("Deploying masters...")

	// Get client
	client, err := noauth.NewBareMetalNoAuth(noauth.EndpointOpts{
		IronicEndpoint: IronicEndpoint,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "client error: %v\n", err)
		os.Exit(1)
	}

	client.Microversion = IronicMicroversion

	// Ignition user data file
	ignition, err := ioutil.ReadFile(IgnitionConfig)
	if err != nil {
		os.Exit(1)
	}

	// Create nodes
	createdNodes, err := createNodes(client, os.Getenv("MASTER_NODES_FILE"))
	if err != nil {
		fmt.Printf("Could not creates nodes: %+v\n", err)
	}

	// Deploy nodes
	fmt.Printf("Deploying nodes...\n\n")

	uiprogress.Start()

	var wg sync.WaitGroup
	var errors []error
	var mux sync.Mutex

	for _, node := range createdNodes {
		wg.Add(1)
		go func(node nodes.Node) {
			defer wg.Done()
			var deployment deploy.Deployment

			status := make(chan int)
			deployment = deploy.Deployment{
				NodeUUID: node.UUID,
				UpdateOpts: nodes.UpdateOpts{
					nodes.UpdateOperation{
						Op:   nodes.AddOp,
						Path: "/instance_info",
						Value: map[string]string{
							"image_source":   "http://172.22.0.1/images/" + os.Getenv("RHCOS_IMAGE_FILENAME_DUALDHCP"),
							"image_checksum": os.Getenv("RHCOS_IMAGE_CHECKSUM"),
							"root_gb":        "25",
						},
					},
					nodes.UpdateOperation{
						Op:   nodes.AddOp,
						Path: "/properties",
						Value: map[string]string{
							"name": os.Getenv("ROOT_DISK"),
						},
					},
				},
				ConfigDrive: deploy.ConfigDrive{
					UserData: deploy.UserDataString(ignition)},
			}

			mux.Lock()
			bar := uiprogress.AddBar(100).AppendCompleted()
			mux.Unlock()

			bar.PrependFunc(func(b *uiprogress.Bar) string {
				return node.Name
			})

			go deploy.Deploy(client, &deployment, status)

			for x := range status {
				bar.Set(x)
				if deployment.Error != nil {
					mux.Lock()
					errors = append(errors, deployment.Error)
					mux.Unlock()

					bar.AppendFunc(func(b *uiprogress.Bar) string {
						return "FAIL"
					})
				}
			}

		}(node)
	}

	wg.Wait()
	uiprogress.Stop()
	if len(errors) > 0 {
		fmt.Println("Errors were encountered!\n\n")
		for _, err := range errors {
			fmt.Println(err.Error())
		}
		os.Exit(1)
	}
}

// Create nodes from JSON node definition file, as defined by tripleo's Ansible template.  This template includes a
// definition for ports, which is not valid in a single call to the Ironic API.  We deserialize it as CreateOpts,
// and then again to extract the ports.
func createNodes(client *gophercloud.ServiceClient, path string) ([]nodes.Node, error) {
	var createdNodes []nodes.Node

	var createOpts struct {
		Opts []nodes.CreateOpts `json:"nodes"`
	}

	var portOpts struct {
		Nodes []struct {
			Name  string             `json:"name"`
			Ports []ports.CreateOpts `json:"ports"`
		} `json:"nodes"`
	}

	// Read file
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Unmarshal CreateOpts
	if err := json.Unmarshal(contents, &createOpts); err != nil {
		return nil, err
	}

	// Unmarshal ports
	if err := json.Unmarshal(contents, &portOpts); err != nil {
		return nil, err
	}

	for _, node := range createOpts.Opts {
		fmt.Printf("Creating node %s\n", node.Name)
		node, err := nodes.Create(client, node).Extract()
		if err != nil {
			return nil, err
		}

		createdNodes = append(createdNodes, *node)

		// Create port for node
		for _, portNodes := range portOpts.Nodes {
			if portNodes.Name == node.Name {
				for _, port := range portNodes.Ports {
					port.NodeUUID = node.UUID
					_, err := ports.Create(client, port).Extract()
					if err != nil {
						return nil, err
					}
				}
			}
		}

	}

	return createdNodes, nil
}
