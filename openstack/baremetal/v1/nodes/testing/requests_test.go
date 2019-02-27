package testing

import (
	"fmt"
	"testing"
	th "github.com/gophercloud/gophercloud/testhelper"
)

func TestUserDataFromMap(t *testing.T) {
	userData, err := IgnitionUserData.ToUserData()
	th.AssertNoErr(t, err)
	fmt.Println(IgnitionConfig)
	fmt.Println(userData)
	th.CheckJSONEquals(t, IgnitionConfig, userData)
}