// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

// Command dmtest is a small driver around github.com/go-fsctl/dm used to
// validate the library against the real kernel. It is not part of the public
// API; it exists for the integration walkthrough in the README and CI.
//
// Usage:
//
//	dmtest version
//	dmtest create  <name> [uuid]
//	dmtest linear  <name> <backing-dev> <sectors>   # load 1 linear target + resume
//	dmtest striped <name> <chunk> <sectors> <dev0> <off0> <dev1> <off1> [...]
//	dmtest zero    <name> <sectors>
//	dmtest error   <name> <sectors>
//	dmtest snapshot <name> <origin> <cow> <P|N> <chunk> <sectors>
//	dmtest snaporigin <name> <origin> <sectors>
//	dmtest snapstatus <name>
//	dmtest crypt   <name> <cipher> <hexkey> <iv> <dev> <off> <sectors> [opt...]
//	dmtest thinpool <name> <meta> <data> <datablocksectors> <lowwater> <sectors> [opt...]
//	dmtest createthin <pool> <devid>
//	dmtest createsnap <pool> <devid> <originid>
//	dmtest deletethin <pool> <devid>
//	dmtest thin    <name> <pool> <devid> <sectors> [external-origin]
//	dmtest thinpoolstatus <name>
//	dmtest thinstatus <name>
//	dmtest message <name> <sector> <msg...>
//	dmtest verity  <name> <ver> <datadev> <hashdev> <dbs> <hbs> <ndb> <hsb> <algo> <root> <salt> <sectors> [opt...]
//	dmtest info    <name>
//	dmtest table   <name>
//	dmtest status  <name>
//	dmtest list
//	dmtest remove  <name>
package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/go-fsctl/dm"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "version":
		v, err := dm.Version()
		check(err)
		fmt.Printf("device-mapper interface version: %s\n", v)

	case "create":
		need(3)
		uuid := ""
		if len(os.Args) > 3 {
			uuid = os.Args[3]
		}
		check(dm.Create(os.Args[2], uuid))
		fmt.Printf("created %s\n", os.Args[2])

	case "linear":
		need(5)
		sectors, err := strconv.ParseUint(os.Args[4], 10, 64)
		check(err)
		name, backing := os.Args[2], os.Args[3]
		check(dm.LoadTable(name, []dm.Target{dm.Linear(0, sectors, backing, 0)}))
		check(dm.Resume(name))
		fmt.Printf("loaded+resumed %s: linear %s 0 (%d sectors)\n", name, backing, sectors)

	case "striped":
		// striped <name> <chunk> <sectors> <dev0> <off0> [<dev1> <off1> ...]
		need(7)
		chunk := mustUint(os.Args[3])
		sectors := mustUint(os.Args[4])
		rest := os.Args[5:]
		if len(rest)%2 != 0 {
			usage()
		}
		var devs []dm.StripeDev
		for i := 0; i < len(rest); i += 2 {
			devs = append(devs, dm.StripeDev{Dev: rest[i], Offset: mustUint(rest[i+1])})
		}
		check(dm.CreateWithTable(os.Args[2], []dm.Target{dm.Striped(0, sectors, chunk, devs)}))
		fmt.Printf("created+resumed %s: striped %d devs chunk %d (%d sectors)\n", os.Args[2], len(devs), chunk, sectors)

	case "zero":
		need(4)
		sectors := mustUint(os.Args[3])
		check(dm.CreateWithTable(os.Args[2], []dm.Target{dm.Zero(0, sectors)}))
		fmt.Printf("created+resumed %s: zero (%d sectors)\n", os.Args[2], sectors)

	case "error":
		need(4)
		sectors := mustUint(os.Args[3])
		check(dm.CreateWithTable(os.Args[2], []dm.Target{dm.Error(0, sectors)}))
		fmt.Printf("created+resumed %s: error (%d sectors)\n", os.Args[2], sectors)

	case "snapshot":
		// snapshot <name> <origin> <cow> <P|N> <chunk> <sectors>
		need(8)
		persistent := os.Args[5] == "P" || os.Args[5] == "p"
		chunk := mustUint(os.Args[6])
		sectors := mustUint(os.Args[7])
		tgt := dm.Snapshot(0, sectors, os.Args[3], os.Args[4], persistent, chunk)
		check(dm.CreateWithTable(os.Args[2], []dm.Target{tgt}))
		fmt.Printf("created+resumed %s: %s\n", os.Args[2], tgt.String())

	case "snaporigin":
		// snaporigin <name> <origin> <sectors>
		need(5)
		sectors := mustUint(os.Args[4])
		tgt := dm.SnapshotOrigin(0, sectors, os.Args[3])
		check(dm.CreateWithTable(os.Args[2], []dm.Target{tgt}))
		fmt.Printf("created+resumed %s: %s\n", os.Args[2], tgt.String())

	case "snapstatus":
		need(3)
		s, err := dm.SnapshotStatus(os.Args[2])
		check(err)
		fmt.Printf("valid=%v used=%d total=%d metadata=%d raw=%q\n",
			s.Valid, s.UsedSectors, s.TotalSectors, s.MetadataSectors, s.Raw)

	case "crypt":
		// crypt <name> <cipher> <hexkey> <iv> <dev> <off> <sectors> [opt...]
		need(9)
		key, err := hex.DecodeString(os.Args[4])
		check(err)
		iv := mustUint(os.Args[5])
		dev := os.Args[6]
		off := mustUint(os.Args[7])
		sectors := mustUint(os.Args[8])
		var opts []string
		if len(os.Args) > 9 {
			opts = os.Args[9:]
		}
		tgt := dm.Crypt(0, sectors, os.Args[3], key, iv, dev, off, opts)
		check(dm.CreateWithTable(os.Args[2], []dm.Target{tgt}))
		fmt.Printf("created+resumed %s: crypt %s ... %s %d (%d sectors)\n", os.Args[2], os.Args[3], dev, off, sectors)

	case "thinpool":
		// thinpool <name> <meta> <data> <datablocksectors> <lowwater> <sectors> [opt...]
		need(8)
		meta, data := os.Args[3], os.Args[4]
		dbs := mustUint(os.Args[5])
		low := mustUint(os.Args[6])
		sectors := mustUint(os.Args[7])
		var opts []string
		if len(os.Args) > 8 {
			opts = os.Args[8:]
		}
		tgt := dm.ThinPool(0, sectors, meta, data, dbs, low, opts)
		check(dm.CreateWithTable(os.Args[2], []dm.Target{tgt}))
		fmt.Printf("created+resumed %s: %s\n", os.Args[2], tgt.String())

	case "createthin":
		need(4)
		check(dm.ThinPoolCreateThin(os.Args[2], mustUint(os.Args[3])))
		fmt.Printf("created thin id %s in pool %s\n", os.Args[3], os.Args[2])

	case "createsnap":
		need(5)
		check(dm.ThinPoolCreateSnap(os.Args[2], mustUint(os.Args[3]), mustUint(os.Args[4])))
		fmt.Printf("created snap id %s of origin %s in pool %s\n", os.Args[3], os.Args[4], os.Args[2])

	case "deletethin":
		need(4)
		check(dm.ThinPoolDeleteThin(os.Args[2], mustUint(os.Args[3])))
		fmt.Printf("deleted thin id %s from pool %s\n", os.Args[3], os.Args[2])

	case "thin":
		// thin <name> <pool> <devid> <sectors> [external-origin]
		need(6)
		pool := os.Args[3]
		devid := mustUint(os.Args[4])
		sectors := mustUint(os.Args[5])
		ext := ""
		if len(os.Args) > 6 {
			ext = os.Args[6]
		}
		tgt := dm.Thin(0, sectors, pool, devid, ext)
		check(dm.CreateWithTable(os.Args[2], []dm.Target{tgt}))
		fmt.Printf("created+resumed %s: %s\n", os.Args[2], tgt.String())

	case "thinpoolstatus":
		need(3)
		s, err := dm.ThinPoolStatus(os.Args[2])
		check(err)
		fmt.Printf("ok=%v txn=%d meta=%d/%d data=%d/%d raw=%q\n",
			s.OK, s.TransactionID, s.UsedMetaBlocks, s.TotalMetaBlocks,
			s.UsedDataBlocks, s.TotalDataBlocks, s.Raw)

	case "thinstatus":
		need(3)
		s, err := dm.ThinStatus(os.Args[2])
		check(err)
		fmt.Printf("ok=%v mapped=%d highest=%d hashighest=%v raw=%q\n",
			s.OK, s.MappedSectors, s.HighestMappedSector, s.HasHighest, s.Raw)

	case "message":
		// message <name> <sector> <msg...>
		need(4)
		sector := mustUint(os.Args[3])
		msg := strings.Join(os.Args[4:], " ")
		reply, err := dm.Message(os.Args[2], sector, msg)
		check(err)
		fmt.Printf("message %q -> reply %q\n", msg, reply)

	case "verity":
		// verity <name> <ver> <datadev> <hashdev> <dbs> <hbs> <ndb> <hsb> <algo> <root> <salt> <sectors> [opt...]
		need(14)
		p := dm.VerityParams{
			Version:        mustUint(os.Args[3]),
			DataDev:        os.Args[4],
			HashDev:        os.Args[5],
			DataBlockSize:  mustUint(os.Args[6]),
			HashBlockSize:  mustUint(os.Args[7]),
			NumDataBlocks:  mustUint(os.Args[8]),
			HashStartBlock: mustUint(os.Args[9]),
			Algorithm:      os.Args[10],
			RootDigest:     os.Args[11],
			Salt:           os.Args[12],
		}
		sectors := mustUint(os.Args[13])
		if len(os.Args) > 14 {
			p.Opts = os.Args[14:]
		}
		tgt := dm.Verity(0, sectors, p)
		// dm-verity requires the device be read-only.
		check(dm.CreateReadOnlyWithTable(os.Args[2], []dm.Target{tgt}))
		fmt.Printf("created+resumed %s: %s\n", os.Args[2], tgt.String())

	case "status":
		need(3)
		tgts, err := dm.Status(os.Args[2])
		check(err)
		for _, t := range tgts {
			fmt.Println(t.String())
		}

	case "info":
		need(3)
		i, err := dm.Info(os.Args[2])
		check(err)
		fmt.Printf("name=%s dev=%d:%d open=%d targets=%d flags=%#x suspended=%v active=%v\n",
			i.Name, i.Major(), i.Minor(), i.OpenCount, i.TargetCnt, i.Flags, i.Suspended(), i.ActivePresent())

	case "table":
		need(3)
		tbl, err := dm.TableStatus(os.Args[2])
		check(err)
		for _, t := range tbl {
			fmt.Println(t.String())
		}

	case "list":
		devs, err := dm.List()
		check(err)
		for _, d := range devs {
			fmt.Printf("%s (dev %d)\n", d.Name, d.Dev)
		}

	case "remove":
		need(3)
		check(dm.Remove(os.Args[2]))
		fmt.Printf("removed %s\n", os.Args[2])

	default:
		usage()
	}
}

func need(n int) {
	if len(os.Args) < n {
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: dmtest <version|create|linear|striped|zero|error|snapshot|snaporigin|snapstatus|crypt|thinpool|createthin|createsnap|deletethin|thin|thinpoolstatus|thinstatus|message|verity|info|table|status|list|remove> ...")
	os.Exit(2)
}

func mustUint(s string) uint64 {
	v, err := strconv.ParseUint(s, 10, 64)
	check(err)
	return v
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
