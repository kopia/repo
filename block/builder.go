package block

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
)

// packIndexBuilder prepares and writes block index for writing.
type packIndexBuilder map[string]*Info

// Add adds a new entry to the builder or conditionally replaces it if the timestamp is greater.
func (b packIndexBuilder) Add(i Info) {
	old, ok := b[i.BlockID]
	if !ok || i.TimestampSeconds >= old.TimestampSeconds {
		b[i.BlockID] = &i
	}
}

func (b packIndexBuilder) sortedBlocks() []*Info {
	var allBlocks []*Info

	for _, v := range b {
		allBlocks = append(allBlocks, v)
	}

	sort.Slice(allBlocks, func(i, j int) bool {
		return allBlocks[i].BlockID < allBlocks[j].BlockID
	})

	return allBlocks
}

type indexLayout struct {
	packFileOffsets map[string]uint32
	entryCount      int
	keyLength       int
	entryLength     int
	extraDataOffset uint32
}

// Build writes the pack index to the provided output.
func (b packIndexBuilder) Build(output io.Writer) error {
	allBlocks := b.sortedBlocks()
	layout := &indexLayout{
		packFileOffsets: map[string]uint32{},
		keyLength:       -1,
		entryLength:     20,
		entryCount:      len(allBlocks),
	}

	w := bufio.NewWriter(output)

	// prepare extra data to be appended at the end of an index.
	extraData := prepareExtraData(allBlocks, layout)

	// write header
	header := make([]byte, 8)
	header[0] = 1 // version
	header[1] = byte(layout.keyLength)
	binary.BigEndian.PutUint16(header[2:4], uint16(layout.entryLength))
	binary.BigEndian.PutUint32(header[4:8], uint32(layout.entryCount))
	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("unable to write header: %v", err)
	}

	// write all sorted blocks.
	entry := make([]byte, layout.entryLength)
	for _, it := range allBlocks {
		if err := writeEntry(w, it, layout, entry); err != nil {
			return fmt.Errorf("unable to write entry: %v", err)
		}
	}

	if _, err := w.Write(extraData); err != nil {
		return fmt.Errorf("error writing extra data: %v", err)
	}

	return w.Flush()
}

func prepareExtraData(allBlocks []*Info, layout *indexLayout) []byte {
	var extraData []byte

	for i, it := range allBlocks {
		if i == 0 {
			layout.keyLength = len(contentIDToBytes(it.BlockID))
		}
		if it.PackFile != "" {
			if _, ok := layout.packFileOffsets[it.PackFile]; !ok {
				layout.packFileOffsets[it.PackFile] = uint32(len(extraData))
				extraData = append(extraData, []byte(it.PackFile)...)
			}
		}
		if len(it.Payload) > 0 {
			panic("storing payloads in indexes is not supported")
		}
	}
	layout.extraDataOffset = uint32(8 + layout.entryCount*(layout.keyLength+layout.entryLength))
	return extraData
}

func writeEntry(w io.Writer, it *Info, layout *indexLayout, entry []byte) error {
	k := contentIDToBytes(it.BlockID)
	if len(k) != layout.keyLength {
		return fmt.Errorf("inconsistent key length: %v vs %v", len(k), layout.keyLength)
	}

	if err := formatEntry(entry, it, layout); err != nil {
		return fmt.Errorf("unable to format entry: %v", err)
	}

	if _, err := w.Write(k); err != nil {
		return fmt.Errorf("error writing entry key: %v", err)
	}
	if _, err := w.Write(entry); err != nil {
		return fmt.Errorf("error writing entry: %v", err)
	}

	return nil
}

func formatEntry(entry []byte, it *Info, layout *indexLayout) error {
	entryTimestampAndFlags := entry[0:8]
	entryPackFileOffset := entry[8:12]
	entryPackedOffset := entry[12:16]
	entryPackedLength := entry[16:20]
	timestampAndFlags := uint64(it.TimestampSeconds) << 16

	if len(it.PackFile) == 0 {
		return fmt.Errorf("empty pack block ID for %v", it.BlockID)
	}

	binary.BigEndian.PutUint32(entryPackFileOffset, layout.extraDataOffset+layout.packFileOffsets[it.PackFile])
	if it.Deleted {
		binary.BigEndian.PutUint32(entryPackedOffset, it.PackOffset|0x80000000)
	} else {
		binary.BigEndian.PutUint32(entryPackedOffset, it.PackOffset)
	}
	binary.BigEndian.PutUint32(entryPackedLength, it.Length)
	timestampAndFlags |= uint64(it.FormatVersion) << 8
	timestampAndFlags |= uint64(len(it.PackFile))
	binary.BigEndian.PutUint64(entryTimestampAndFlags, timestampAndFlags)
	return nil
}
