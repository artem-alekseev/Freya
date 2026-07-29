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
	<cabal_world>
		<world id="1">
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
	if stats.Worlds != 1 || stats.Entries != 1 || stats.Books != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	book, ok := FindSkillBook(1, 2, 2)
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
	if stats.Worlds == 0 || stats.Entries == 0 || stats.Books == 0 {
		t.Fatalf("client DEC files contain no skill books: %+v", stats)
	}
}
