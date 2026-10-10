package vision

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Valve Pak (VPK) reading, limited to what portrait extraction needs: open the
// directory archive, enumerate its tree, and read the bytes of one entry out of
// the numbered data archive that holds it.
//
// This reads static files Dota ships, offline, the same way any file manager
// would. It never touches the running game: not its process, not its memory, and
// it automates no input.
const (
	vpkMagic         = 0x55aa1234
	archiveIndexDir  = 0x7fff // entry data lives in the _dir archive itself
	vpkEntryTerminal = 0xffff
)

type vpkEntry struct {
	archiveIndex uint16
	offset       uint32
	length       uint32
	preload      []byte
}

// VPK is an opened Dota asset archive.
type VPK struct {
	dirPath       string
	prefix        string
	fileDataStart int64
	entries       map[string]vpkEntry
	order         []string

	dirFile  *os.File
	archives map[uint16]*os.File
}

// OpenVPK opens the Dota asset archive inside dotaPath (the .../game/dota
// directory that holds pak01_dir.vpk).
func OpenVPK(dotaPath string) (*VPK, error) {
	dirPath := filepath.Join(dotaPath, "pak01_dir.vpk")
	f, err := os.Open(dirPath)
	if err != nil {
		return nil, fmt.Errorf("vpk: open %s: %w", dirPath, err)
	}
	defer f.Close()

	head := make([]byte, 28)
	if _, err := io.ReadFull(f, head); err != nil {
		return nil, fmt.Errorf("vpk: read header: %w", err)
	}
	if binary.LittleEndian.Uint32(head[0:4]) != vpkMagic {
		return nil, fmt.Errorf("vpk: %s is not a VPK (bad magic)", dirPath)
	}
	version := binary.LittleEndian.Uint32(head[4:8])
	treeSize := binary.LittleEndian.Uint32(head[8:12])

	var headerSize int64
	switch version {
	case 1:
		headerSize = 12
	case 2:
		headerSize = 28
	default:
		return nil, fmt.Errorf("vpk: unsupported version %d", version)
	}

	tree := make([]byte, treeSize)
	if _, err := f.ReadAt(tree, headerSize); err != nil {
		return nil, fmt.Errorf("vpk: read tree: %w", err)
	}

	entries, order, err := parseVPKTree(tree)
	if err != nil {
		return nil, err
	}

	prefix := strings.TrimSuffix(filepath.Base(dirPath), "_dir.vpk")
	return &VPK{
		dirPath:       dirPath,
		prefix:        prefix,
		fileDataStart: headerSize + int64(treeSize),
		entries:       entries,
		order:         order,
		archives:      map[uint16]*os.File{},
	}, nil
}

// Entries returns every path in the archive, sorted for deterministic walks.
func (v *VPK) Entries() []string {
	out := append([]string(nil), v.order...)
	sort.Strings(out)
	return out
}

// Read returns the bytes of one archive entry.
func (v *VPK) Read(path string) ([]byte, error) {
	e, ok := v.entries[path]
	if !ok {
		return nil, fmt.Errorf("vpk: %s not found", path)
	}
	out := make([]byte, 0, len(e.preload)+int(e.length))
	out = append(out, e.preload...)
	if e.length == 0 {
		return out, nil
	}

	var f *os.File
	var base int64
	if e.archiveIndex == archiveIndexDir {
		var err error
		f, err = v.dir()
		if err != nil {
			return nil, err
		}
		base = v.fileDataStart
	} else {
		var err error
		f, err = v.archive(e.archiveIndex)
		if err != nil {
			return nil, err
		}
	}
	buf := make([]byte, e.length)
	if _, err := f.ReadAt(buf, base+int64(e.offset)); err != nil {
		return nil, fmt.Errorf("vpk: read %s: %w", path, err)
	}
	out = append(out, buf...)
	return out, nil
}

// Close releases the archive file handles.
func (v *VPK) Close() {
	if v.dirFile != nil {
		v.dirFile.Close()
		v.dirFile = nil
	}
	for _, f := range v.archives {
		f.Close()
	}
	v.archives = map[uint16]*os.File{}
}

func (v *VPK) dir() (*os.File, error) {
	if v.dirFile != nil {
		return v.dirFile, nil
	}
	f, err := os.Open(v.dirPath)
	if err != nil {
		return nil, err
	}
	v.dirFile = f
	return f, nil
}

func (v *VPK) archive(index uint16) (*os.File, error) {
	if f, ok := v.archives[index]; ok {
		return f, nil
	}
	name := fmt.Sprintf("%s_%03d.vpk", v.prefix, index)
	path := filepath.Join(filepath.Dir(v.dirPath), name)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("vpk: open archive %s: %w", name, err)
	}
	v.archives[index] = f
	return f, nil
}

type vpkCursor struct {
	b   []byte
	pos int
}

func (c *vpkCursor) cstring() (string, error) {
	start := c.pos
	for c.pos < len(c.b) && c.b[c.pos] != 0 {
		c.pos++
	}
	if c.pos >= len(c.b) {
		return "", io.ErrUnexpectedEOF
	}
	s := string(c.b[start:c.pos])
	c.pos++
	return s, nil
}

func (c *vpkCursor) u16() (uint16, error) {
	if c.pos+2 > len(c.b) {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.LittleEndian.Uint16(c.b[c.pos : c.pos+2])
	c.pos += 2
	return v, nil
}

func (c *vpkCursor) u32() (uint32, error) {
	if c.pos+4 > len(c.b) {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.LittleEndian.Uint32(c.b[c.pos : c.pos+4])
	c.pos += 4
	return v, nil
}

func (c *vpkCursor) take(n int) ([]byte, error) {
	if c.pos+n > len(c.b) {
		return nil, io.ErrUnexpectedEOF
	}
	out := c.b[c.pos : c.pos+n]
	c.pos += n
	return out, nil
}

func parseVPKTree(tree []byte) (map[string]vpkEntry, []string, error) {
	c := &vpkCursor{b: tree}
	entries := map[string]vpkEntry{}
	var order []string

	for {
		ext, err := c.cstring()
		if err != nil {
			return nil, nil, fmt.Errorf("vpk: tree extension: %w", err)
		}
		if ext == "" {
			break
		}
		for {
			path, err := c.cstring()
			if err != nil {
				return nil, nil, fmt.Errorf("vpk: tree path: %w", err)
			}
			if path == "" {
				break
			}
			for {
				name, err := c.cstring()
				if err != nil {
					return nil, nil, fmt.Errorf("vpk: tree name: %w", err)
				}
				if name == "" {
					break
				}
				if _, err := c.u32(); err != nil { // CRC, unused
					return nil, nil, err
				}
				preloadBytes, err := c.u16()
				if err != nil {
					return nil, nil, err
				}
				archiveIndex, err := c.u16()
				if err != nil {
					return nil, nil, err
				}
				offset, err := c.u32()
				if err != nil {
					return nil, nil, err
				}
				length, err := c.u32()
				if err != nil {
					return nil, nil, err
				}
				terminal, err := c.u16()
				if err != nil {
					return nil, nil, err
				}
				if terminal != vpkEntryTerminal {
					return nil, nil, fmt.Errorf("vpk: bad entry terminator 0x%04x", terminal)
				}
				var preload []byte
				if preloadBytes > 0 {
					preload, err = c.take(int(preloadBytes))
					if err != nil {
						return nil, nil, fmt.Errorf("vpk: preload data: %w", err)
					}
				}
				// The tree stores the extension without its leading dot
				// ("vtex_c", not ".vtex_c"), so the dot is added back here.
				file := name
				if ext != "" {
					file = name + "." + ext
				}
				full := file
				if path != " " && path != "" {
					full = path + "/" + file
				}
				entries[full] = vpkEntry{
					archiveIndex: archiveIndex,
					offset:       offset,
					length:       length,
					preload:      preload,
				}
				order = append(order, full)
			}
		}
	}
	return entries, order, nil
}
