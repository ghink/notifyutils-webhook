package webhook

import "go.gh.ink/notifyutils/driver"

func init() {
	driver.Register(Name, Driver{})
}
