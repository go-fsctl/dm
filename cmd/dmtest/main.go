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
	fmt.Fprintln(os.Stderr, "usage: dmtest <version|create|linear|striped|zero|error|snapshot|snaporigin|snapstatus|crypt|info|table|status|list|remove> ...")
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
