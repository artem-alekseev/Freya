package def

import (
	"github.com/ubis/Freya/share/models/server"
	"github.com/ubis/Freya/share/network"
	"github.com/ubis/Freya/share/rpc"
)

type Settings struct {
	server.Settings
}

var ServerConfig = &Config{}
var ServerSettings = &Settings{}
var NetworkManager = &network.Network{}
var RPCHandler = &rpc.Client{}
