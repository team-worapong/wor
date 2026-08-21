package phpfpm

import (
	"fmt"
	"testing"
)

// The exact bytes the pre-php.ini renderer produced, so an existing host
// upgrading to this version does not see every pooled service reported
// as drifted (and rewritten + reloaded) for no reason.
func TestRenderIsByteIdenticalToThePreSettingsTemplate(t *testing.T) {
	p := Pool{
		Domain: "shop.example.com", Service: "web",
		Version: Version{SockDir: "/run/php"},
		User:    "wor_shop.example.com_web", Group: "wor_shop.example.com_web",
		ListenOwner: "www-data", ListenGroup: "www-data",
	}
	old := fmt.Sprintf(`[%s]
user = %s
group = %s
listen = %s
listen.owner = %s
listen.group = %s
listen.mode = 0660
pm = dynamic
pm.max_children = %d
pm.start_servers = 2
pm.min_spare_servers = 1
pm.max_spare_servers = 3
`, PoolName(p.Domain, p.Service), p.User, p.Group, SocketPath(p.Version, p.Domain, p.Service),
		p.ListenOwner, p.ListenGroup, 5)

	if got := PoolFileContent(p); got != old {
		t.Errorf("rendering changed for a service with no settings files.\n--- old ---\n%s\n--- new ---\n%s", old, got)
	}
}
