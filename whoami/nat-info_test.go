package whoami

import (
	"fmt"
	"testing"
)

func Test_GetNatInfo(t *testing.T) {
	a, b, c, d, e, _ := getNatInfo("1352")
	fmt.Println(a, b, c, d, e)
}
