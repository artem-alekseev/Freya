package main

import (
	"github.com/ubis/Freya/cmd/chatserver/chat"
	"github.com/ubis/Freya/share/event"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/server"
	"github.com/ubis/Freya/share/network"
	"github.com/ubis/Freya/share/rpc"
)

func RegisterEvents() {
	log.Info("Registering ChatNode events...")
	event.Register(event.ClientConnectEvent, event.Handler(onClientConnect))
	event.Register(event.ClientDisconnectEvent, event.Handler(onClientDisconnect))
	event.Register(event.PacketReceiveEvent, event.Handler(onPacketReceive))
	event.Register(event.SyncConnectEvent, event.Handler(onSyncConnect))
	event.Register(event.SyncDisconnectEvent, event.Handler(onSyncDisconnect))
}

func onClientConnect(e *event.Event) {
	rawSession, ok := e.Get()
	if !ok {
		return
	}

	session, ok := rawSession.(*network.Session)
	if !ok {
		return
	}

	session.DataEx = &chat.State{Server: byte(g_ServerConfig.ServerID)}
	log.Infof("ChatNode client `%s` connected", session.GetEndPnt())
}

func onClientDisconnect(e *event.Event) {
	rawSession, ok := e.Get()
	if !ok {
		return
	}

	session, ok := rawSession.(*network.Session)
	if !ok {
		return
	}

	if state, ok := session.DataEx.(*chat.State); ok {
		if state.LoggedIn && state.CharacterID > 0 {
			characterID := state.CharacterID
			state.LoggedIn = false
			chat.NotifyBuddyPresence(state.Server, characterID)
			chat.ClearEquipmentViewRequests(characterID)
		}
		chat.ClearBuddyInvites(state.CharacterID)
	}
	log.Infof("ChatNode client `%s` disconnected", session.GetEndPnt())
}

func onPacketReceive(e *event.Event) {
	rawPacket, ok := e.Get()
	if !ok {
		return
	}

	packet, ok := rawPacket.(*network.PacketArgs)
	if !ok {
		return
	}

	log.Debugf("Received ChatNode packet (Len: %d, type: %d, src: %s)",
		packet.Length, packet.Type, packet.Session.GetEndPnt())
	chat.HandlePacket(packet.Session, packet.Reader)
}

func onSyncConnect(_ *event.Event) {
	request := server.RegisterReq{
		Type:     server.CHAT_SERVER,
		ServerId: byte(g_ServerConfig.ServerID),
	}
	response := server.RegisterRes{}
	if err := g_RPCHandler.Call(rpc.ServerRegister, &request, &response); err != nil {
		log.Errorf("Unable to register ChatServer: %s", err)
		return
	}
	log.Infof("Registered ChatServer for server %d", request.ServerId)
}

func onSyncDisconnect(_ *event.Event) {
	log.Info("Lost connection with the Master Server! Reconnecting...")
}
