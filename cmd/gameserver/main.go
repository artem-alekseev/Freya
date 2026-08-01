package main

import (
	"path/filepath"

	"github.com/ubis/Freya/cmd/gameserver/clientdata"
	"github.com/ubis/Freya/cmd/gameserver/def"
	"github.com/ubis/Freya/cmd/gameserver/game"
	"github.com/ubis/Freya/cmd/gameserver/packet"
	"github.com/ubis/Freya/cmd/gameserver/rpc"
	"github.com/ubis/Freya/share/directory"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/script"
)

// globals
var g_ServerConfig = def.ServerConfig
var g_ServerSettings = def.ServerSettings
var g_NetworkManager = def.NetworkManager
var g_PacketHandler = def.PacketHandler
var g_RPCHandler = def.RPCHandler

func main() {
	log.Init(def.GetName())

	// read config
	g_ServerConfig.Read()

	stats, err := clientdata.Initialize(filepath.Join(directory.Root(), "enc"))
	if err != nil {
		log.Fatal("Unable to load client data: ", err.Error())
		return
	}
	log.Infof("Loaded %d skill books, %d character levels and %d NPC shop items from client DEC files", stats.Books, stats.CharacterLevels, stats.ShopItems)

	game := &game.WorldManager{SpawnMobs: g_ServerConfig.SpawnMobs}
	game.Initialize()

	// set server settings
	g_ServerSettings.XorKeyTable.Init()

	// register events
	RegisterEvents(game)

	// register scripting engine
	script.Initialize(g_ServerConfig.ScriptDirectory)

	// register scripting functions
	packet.RegisterFunc()

	// init packet handler
	g_PacketHandler.Init()

	// register packets
	packet.RegisterPackets()

	// init RPC handler
	g_RPCHandler.Init()
	g_RPCHandler.IpAddress = g_ServerConfig.MasterIp
	g_RPCHandler.Port = g_ServerConfig.MasterPort

	// register RPC calls
	rpc.RegisterCalls()

	// start RPC handler
	g_RPCHandler.Start()

	// create network and start listening for connections
	g_NetworkManager.Init(g_ServerConfig.Port, &g_ServerSettings.Settings)
}
