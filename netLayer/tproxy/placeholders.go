//go:build !(linux || darwin)

package tproxy

import "github.com/e1732a364fed/v2ray_simple/utils"

// placeholder for unsupported systems, return utils.ErrNotImplemented
func SetRouteByPort(port int) error {
	return utils.ErrUnImplemented
}

// placeholder for unsupported systems
func CleanupRoutes() {
}
