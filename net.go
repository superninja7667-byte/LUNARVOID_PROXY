package engine

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/sandertv/go-raknet"
	"github.com/sandertv/gophertunnel/minecraft"
)

// phoneRakNet is a RakNet network that only sends small test packets while
// connecting. On some phone and headset networks the standard biggest test
// packet is rejected ("message too long") and the connection fails before it
// starts. It is written out in full (no borrowed parts from the library's own
// network) so nothing in it can be left unset.
type phoneRakNet struct {
	maxMTU uint16
}

func (n phoneRakNet) DialContext(ctx context.Context, address string) (net.Conn, error) {
	conn, err := raknet.Dialer{MaxMTU: n.maxMTU}.DialContext(ctx, address)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (n phoneRakNet) PingContext(ctx context.Context, address string) ([]byte, error) {
	return raknet.Dialer{MaxMTU: n.maxMTU}.PingContext(ctx, address)
}

// Listen is never used: this network is only for connecting out.
func (n phoneRakNet) Listen(address string) (minecraft.NetworkListener, error) {
	return nil, errors.New("this network is only used for connecting")
}

// dialWithSmallPackets connects to the server using small test packets first.
// If the size check still fails it retries with smaller sizes before giving up.
func dialWithSmallPackets(ctx context.Context, d minecraft.Dialer, remote string) (*minecraft.Conn, error) {
	var lastErr error
	for _, mtu := range []uint16{1200, 1000, 576} {
		conn, err := d.DialContextNetwork(ctx, phoneRakNet{maxMTU: mtu}, remote)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if ctx.Err() != nil || !strings.Contains(err.Error(), "discover mtu") {
			break // a different problem (or we ran out of time): do not keep retrying
		}
	}
	return nil, lastErr
}
