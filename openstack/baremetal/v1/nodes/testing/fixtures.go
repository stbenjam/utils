package testing

import "github.com/gophercloud/utils/openstack/baremetal/v1/nodes"

const IgnitionConfig = `
{
    "ignition": {
        "version": "2.2.0"
    },
    "systemd": {
        "units": [
            {
                "enabled": true,
                "name": "example.service"
            }
        ]
    }
}
`

const CloudInitString = `
#cloud-init

groups:
  - ubuntu: [root,sys]
  - cloud-users
`

var (
	IgnitionUserData = nodes.UserDataMap{
		"ignition": map[string]string{
			"version": "2.2.0",
		},
		"systemd": map[string]interface{}{
			"units": []map[string]interface{}{{
				"name":     "example.service",
				"enabled":  true,
			},
			},
		},
	}

	CloudInitUserData = nodes.UserDataString(CloudInitString)
)
