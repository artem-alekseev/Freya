package packet

import "github.com/ubis/Freya/share/network"

// DurationSvcData handles the CSC_DURATIONSVC_DATA request sent by the client
// and is also sent proactively during character initialization, as in
// WorldSvr.
func DurationSvcData(session *network.Session, reader *network.Reader) {
	sendDurationSvcData(session)
}

func sendDurationSvcData(session *network.Session) {
	packet := network.NewWriter(DURATIONSVC_DATA)
	serviceKind, _ := chargeState(session)
	packet.WriteByte(0)    // RC_OK
	packet.WriteUint16(36) // free-user + premium profiles 1..35
	packet.WriteUint16(0)  // indexInfoCount (no event pools)
	writeDurationSvcProfile(packet, 0, 4, false)
	for index := byte(1); index <= 35; index++ {
		writeDurationSvcProfile(packet, int32(index), 0, index == serviceKind)
	}

	session.Send(packet)
}

func writeDurationSvcProfile(packet *network.Writer, index int32, serviceType int32, premium bool) {
	// DurationSvcBasePoolID (USE_EMS_15TH / USE_EMS_RENEWAL).
	packet.WriteInt32(0) // eventID
	packet.WriteByte(0)  // bApplyNation
	packet.WriteByte(0)  // bMultiplePoolID

	packet.WriteInt32(index)
	packet.WriteInt32(serviceType)
	packet.WriteInt32(0)  // exp
	packet.WriteUint16(0) // appliedBeginLevel
	packet.WriteUint16(0) // appliedEndLevel
	packet.WriteInt32(0)  // skillExp
	packet.WriteInt32(0)  // dropRate
	packet.WriteInt32(0)  // craftExp
	packet.WriteInt32(0)  // craftSuccess
	if premium {
		packet.WriteInt32(1) // inventory: premium tab
		packet.WriteInt32(1) // warehouse: premium tab
	} else {
		packet.WriteInt32(0) // inventory: premium tab
		packet.WriteInt32(0) // warehouse: premium tab
	}
	packet.WriteInt32(0) // jackpotAlz

	if premium {
		packet.WriteInt32(1) // dummy
		packet.WriteInt32(1) // GPS
	} else {
		packet.WriteInt32(0) // dummy
		packet.WriteInt32(0) // GPS
	}

	packet.WriteInt32(0) // inventory2
	packet.WriteInt32(0) // warehouse2
	packet.WriteInt32(0) // warExp
	packet.WriteInt32(0) // petExp

	// USE_PREMIUM_SERVICE_ADD.
	packet.WriteBool(premium) // useGPSFree
	if premium {
		packet.WriteUint16(15)  // agentShopSlotCount: server maximum
		packet.WriteUint16(256) // agentShopStackableCount: server maximum
	} else {
		packet.WriteUint16(0) // agentShopSlotCount
		packet.WriteUint16(0) // agentShopStackableCount
	}
	packet.WriteUint16(0)     // agentShopDuration
	packet.WriteBool(premium) // agentshopCommission / MarketCharge

	// USE_SOUL_ABILITY.
	packet.WriteInt32(0) // abilityExp
	packet.WriteInt32(0) // point
	if premium {
		packet.WriteInt32(1) // effectDummy / efDummy
	} else {
		packet.WriteInt32(0) // effectDummy / efDummy
	}

	// USE_EMS_15TH / USE_PET_EXPANSION.
	packet.WriteInt32(0) // alzDropInc
	if premium {
		packet.WriteInt32(1) // petSlotOpen: second pet slot
	} else {
		packet.WriteInt32(0) // petSlotOpen: second pet slot
	}
}
