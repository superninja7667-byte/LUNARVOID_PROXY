module lunarvoid/engine

go 1.25

// These versions match the old LUNARVOID build exactly (read from its engine).
// The newer go-raknet has the improved MTU probing that the old app relied on.
require (
	github.com/df-mc/go-nethernet v1.0.20-0.20260912144515-1248f352d699
	github.com/df-mc/go-playfab/v2 v2.0.3
	github.com/df-mc/go-xsapi/v2 v2.0.4-0.20260911132654-00ba4fd7f5c0
	github.com/pion/webrtc/v4 v4.2.16-0.20260627075746-7a223a6f4d4f
	github.com/sandertv/go-raknet v1.15.2-0.20260705184311-0d1fd09e2cf6
	github.com/sandertv/gophertunnel v1.62.0
	golang.org/x/net v0.50.0
	golang.org/x/oauth2 v0.36.0
	golang.org/x/sys v0.41.0
)
