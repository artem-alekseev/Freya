package clientdata

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestInitializeSkillBooks(t *testing.T) {
	directory := t.TempDir()
	itemData := make([]byte, 396+2*400)

	copy(itemData[396+itemNameOffset:], "item1\x00")
	copy(itemData[396+400+itemNameOffset:], "item2\x00")
	binary.LittleEndian.PutUint32(itemData[396+itemTypeOffset:], skillBookItemType)
	binary.LittleEndian.PutUint32(itemData[396+skillBookPriceOffset:], 800)
	binary.LittleEndian.PutUint32(itemData[396+skillBookSkillOffset:], 209)

	if err := os.WriteFile(filepath.Join(directory, "item.dec"), itemData, 0600); err != nil {
		t.Fatal(err)
	}

	cabalData := []byte(`[Font]
fs00=font
<cabal>
	<level_up>
		<con level="1" exp="270" accuexp="270" />
		<con level="2" exp="1080" accuexp="1350" />
	</level_up>
	<skill_order id="209" start_level="1" end_level="3" train_type="9" untrain_price="80" />
	<cabal_world>
		<world id="2">
			<shop id="4">
				<item slot_id="0" item_id="61" option="0" price="400" />
			</shop>
			<trainer id="2">
				<skill slot_id="2" id="209" level="1" skill_book="1" />
			</trainer>
		</world>
	</cabal_world>
</cabal>`)
	if err := os.WriteFile(filepath.Join(directory, "cabal.dec"), cabalData, 0600); err != nil {
		t.Fatal(err)
	}

	stats, err := Initialize(directory)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Worlds != 1 || stats.Entries != 1 || stats.Books != 1 || stats.CharacterLevels != 2 || stats.ShopItems != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	book, ok := FindSkillBook(2, 2, 2)
	if !ok {
		t.Fatal("skill book was not loaded")
	}
	if book.SkillID != 209 || book.ItemID != 1 || book.Price != 800 || book.Level != 1 {
		t.Fatalf("unexpected skill book: %+v", book)
	}
	itemBook, ok := FindSkillBookItem(1)
	if !ok || itemBook.SkillID != 209 {
		t.Fatalf("skill book item lookup failed: %+v", itemBook)
	}
	level, ok := FindSkillLevel(209, 2)
	if !ok || level.UntrainPrice != 80 || level.TrainType != 9 {
		t.Fatalf("skill level lookup failed: %+v", level)
	}
	shopItem, ok := FindShopItem(2, 4, 0)
	if !ok || shopItem.ItemID != 61 || shopItem.Price != 400 {
		t.Fatalf("shop item lookup failed: %+v", shopItem)
	}
	characterLevel, ok := FindCharacterLevel(1)
	if !ok || characterLevel.AccumulatedExp != 270 || LevelForExperience(1, 270) != 2 || LevelForExperience(2, 99999) != 2 {
		t.Fatalf("character level lookup failed: %+v", characterLevel)
	}
}

func TestRepositorySkillBooks(t *testing.T) {
	directory := filepath.Join("..", "..", "..", "enc")
	if _, err := os.Stat(filepath.Join(directory, "item.dec")); os.IsNotExist(err) {
		t.Skip("client DEC files are not installed")
	}

	stats, err := Initialize(directory)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Worlds == 0 || stats.Entries == 0 || stats.Books == 0 || stats.SkillLevels == 0 || stats.CharacterLevels == 0 || stats.ShopItems == 0 {
		t.Fatalf("client DEC files contain no skill books: %+v", stats)
	}
}
