//go:build !testhooks

package transaction

func TestFailpointFromEnvironment() Failpoint {
	return nil
}
