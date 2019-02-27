package nodes

import (
	"encoding/json"
)

// A ConfigDrive struct will be used to create a base64-encoded, gzipped ISO9660 image for use with Ironic.
type ConfigDrive struct {
	UserData    UserDataBuilder        `json:"user_data"`
	MetaData    map[string]interface{} `json:"meta_data"`
	NetworkData map[string]interface{} `json:"network_data"`
}

// UserData may be a string, or JSON data
type UserDataBuilder interface {
	ToUserData() (string, error)
}

type UserDataMap map[string]interface{}
type UserDataString string

// Converts a user data map to JSON-string
func (data UserDataMap) ToUserData() (string, error) {
	bytes, err := json.MarshalIndent(data, "", "    ")
	return string(bytes), err
}

func (data UserDataString) ToUserData() (string, error) {
	return string(data), nil
}

type ConfigDriveBuilder interface {
	ToConfigDrive() (string, error)
}
