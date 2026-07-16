//go:build testhooks

package transaction

import (
	"fmt"
	"os"
	"strings"
)

func TestFailpointFromEnvironment() Failpoint {
	target := os.Getenv("VAULT_RENAME_TEST_FAILPOINT")
	crashTarget := os.Getenv("VAULT_RENAME_TEST_CRASHPOINT")
	if (target == "" && crashTarget == "") || os.Getenv("VAULT_RENAME_TESTING") != "1" {
		return nil
	}
	return func(name string) error {
		if strings.EqualFold(crashTarget, name) {
			os.Exit(97)
		}
		if strings.EqualFold(target, name) {
			return fmt.Errorf("test failpoint %s", name)
		}
		return nil
	}
}
