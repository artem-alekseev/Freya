package rpc

// Server related RPC's
const (
	ServerRegister = "ServerRegister"
	ServerList     = "ServerList"
)

// Account related RPC's
const (
	AuthCheck       = "AuthCheck"
	UserVerify      = "UserVerify"
	PasswdCheck     = "PasswdCheck"
	OnlineCheck     = "OnlineCheck"
	ForceDisconnect = "ForceDisconnect"
)

// SubPassword related RPC's
const (
	FetchSubPassword  = "FetchSubPassword"
	SetSubPassword    = "SetSubPassword"
	RemoveSubPassword = "RemoveSubPassword"
)

// Character related RPC's
const (
	LoadCharacters      = "LoadCharacters"
	CreateCharacter     = "CreateCharacter"
	DeleteCharacter     = "DeleteCharacter"
	SetSlotOrder        = "SetSlotOrder"
	LoadCharacterData   = "LoadCharacterData"
	OpenQuest           = "OpenQuest"
	SaveQuestUI         = "SaveQuestUI"
	SaveExperience      = "SaveExperience"
	SaveSkillExperience = "SaveSkillExperience"
	SaveStat            = "SaveStat"
	SaveVitals          = "SaveVitals"
	SavePosition        = "SavePosition"
	LoadMobDropList     = "LoadMobDropList"
)

// Inventory related RPC's
const (
	EquipItem         = "EquipItem"
	UnEquipItem       = "UnEquipItem"
	SwapEquipmentItem = "SwapEquipmentItem"
	MoveEquipmentItem = "MoveEquipmentItem"
	AddItem           = "AddItem"
	PurchaseItem      = "PurchaseItem"
	SellItems         = "SellItems"
	StackItem         = "StackItem"
	ConsumeItem       = "ConsumeItem"
	RemoveItem        = "RemoveItem"
	SwapItem          = "SwapItem"
	MoveItem          = "MoveItem"
	StorageMove       = "StorageMove"
	EnchantItem       = "EnchantItem"
	ForceCoreEnchant  = "ForceCoreEnchant"
	AttachItem        = "AttachItem"
	LoadCashInventory = "LoadCashInventory"
	AddCashItems      = "AddCashItems"
	UseCashItem       = "UseCashItem"
)

// Skill related RPC's
const (
	LearnSkill            = "LearnSkill"
	SaveSkill             = "SaveSkill"
	UntrainSkill          = "UntrainSkill"
	GrantBattleModeSkills = "GrantBattleModeSkills"
	QuickLinkSet          = "QuickLinkSet"
	QuickLinkRemove       = "QuickLinkRemove"
	QuickLinkSwap         = "QuickLinkSwap"
)
