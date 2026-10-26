// want: dot import of net/http
package fixture

import . "net/http"

func f() { _, _ = Get("https://x") }
