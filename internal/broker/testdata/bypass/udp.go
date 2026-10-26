// Reviewer case: a UDP socket.
// want: import of net is not on the allowlist
package fixture

import "net"

func f() {
	c, _ := net.ListenUDP("udp", nil)
	_, _ = c.WriteTo([]byte("x"), &net.UDPAddr{Port: 53})
}
