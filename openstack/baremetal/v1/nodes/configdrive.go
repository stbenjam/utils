package nodes

import (
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
)

// A ConfigDrive struct will be used to create a base64-encoded, gzipped ISO9660 image for use with Ironic.
type ConfigDrive struct {
	UserData    UserDataBuilder        `json:"user_data"`
	MetaData    map[string]interface{} `json:"meta_data"`
	NetworkData map[string]interface{} `json:"network_data"`
}

// UserData may be a string, or JSON data
type UserDataBuilder interface {
	ToUserData() ([]byte, error)
}

type UserDataMap map[string]interface{}
type UserDataString string

// Converts a user data map to JSON-string
func (data UserDataMap) ToUserData() ([]byte, error) {
	return json.MarshalIndent(data, "", "    ")
}

func (data UserDataString) ToUserData() ([]byte, error) {
	return []byte(data), nil
}

type ConfigDriveBuilder interface {
	ToConfigDrive() (string, error)
}

func (configDrive ConfigDrive) ToConfigDrive() (string, error) {
	// Create a temporary directory for our config drive
	directory, err := ioutil.TempDir("", "gophercloud")
	if err != nil {
		return "", err
	}
	//defer os.RemoveAll(directory)

	// Build up the paths for OpenStack TODO: this should include version information
	path := filepath.FromSlash(directory + "/openstack/latest")
	if err := os.MkdirAll(path, 0755); err != nil {
		return "", err
	}

	// Dump out user data
	if configDrive.UserData != nil {
		userDataPath := filepath.FromSlash(path + "/user_data")
		data, err := configDrive.UserData.ToUserData()
		if err != nil {
			return "", err
		}

		if err := ioutil.WriteFile(userDataPath, data, 0644); err != nil {
	 		return "", err
		}
	}

	// Dump out meta data
	if configDrive.MetaData != nil {
		metaDataPath := filepath.FromSlash(path + "/meta_data.json")
		data, err := json.Marshal(configDrive.MetaData)
		if err != nil {
			return "", err
		}

		if err := ioutil.WriteFile(metaDataPath, data, 0644); err != nil {
	 		return "", err
		}
	}

	// Dump out network data
	if configDrive.NetworkData != nil {
		networkDataPath := filepath.FromSlash(path + "/network_data.json")
		data, err := json.Marshal(configDrive.NetworkData)
		if err != nil {
			return "", err
		}

		if err := ioutil.WriteFile(networkDataPath, data, 0644); err != nil {
	 		return "", err
		}
	}

	// Pack result as gzipped ISO9660 file
	result, err := PackDirectoryAsISO(directory)
	if err != nil {
		return "", err
	}

	// Return as base64-encoded data
	return base64.StdEncoding.EncodeToString(result), nil
}
