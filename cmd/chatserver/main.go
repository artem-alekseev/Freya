package main

import (
	"github.com/ubis/Freya/cmd/chatserver/chat"
	"github.com/ubis/Freya/cmd/chatserver/def"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/rpc"
)

var g_ServerConfig = def.ServerConfig
var g_ServerSettings = def.ServerSettings
var g_NetworkManager = def.NetworkManager
var g_RPCHandler = def.RPCHandler

func main() {
	log.Init("ChatServer")
	g_ServerConfig.Read()
	g_ServerSettings.XorKeyTable.Init()
	g_RPCHandler.Init()
	g_RPCHandler.IpAddress = g_ServerConfig.MasterIp
	g_RPCHandler.Port = g_ServerConfig.MasterPort
	g_RPCHandler.Register(rpc.NotifyMailReceived, chat.NotifyMailReceived)
	chat.SetNetworkManager(g_NetworkManager)
	chat.SetRPCHandler(g_RPCHandler)
	RegisterEvents()
	g_RPCHandler.Start()

	// ChatNode has its own client socket and protocol event loop.
	g_NetworkManager.Init(g_ServerConfig.Port, &g_ServerSettings.Settings)
}
