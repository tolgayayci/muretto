// want: Muretto's server may only be used by itself
package fixture

import "github.com/tolgayayci/muretto/server/testserver"

var _ = testserver.New
