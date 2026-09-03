//go:build !paid

package tests

// Without the `paid` build tag there is no paid account in this binary at all.
//
// The tests that use one are not compiled in, nothing signs it in, and nothing
// looks at it. That is what makes an ordinary `just test` unable to reach an
// account somebody depends on, whatever happens to be in the environment - a
// stronger promise than a skip, which is one edit away from not skipping.

const paidBuild = false

func snapshotPaid()     {}
func comparePaid() bool { return true }
