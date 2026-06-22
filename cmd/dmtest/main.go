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
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/go-fsctl/dm"
)

// Seams over the dm package and process exit, overridable in tests. Production
// code uses the real implementations assigned here.
var (
	version                 = dm.Version
	create                  = dm.Create
	loadTable               = dm.LoadTable
	resume                  = dm.Resume
	createWithTable         = dm.CreateWithTable
	createReadOnlyWithTable = dm.CreateReadOnlyWithTable
	snapshotStatus          = dm.SnapshotStatus
	thinPoolCreateThin      = dm.ThinPoolCreateThin
	thinPoolCreateSnap      = dm.ThinPoolCreateSnap
	thinPoolDeleteThin      = dm.ThinPoolDeleteThin
	thinPoolStatus          = dm.ThinPoolStatus
	thinStatus              = dm.ThinStatus
	message                 = dm.Message
	status                  = dm.Status
	info                    = dm.Info
	tableStatus             = dm.TableStatus
	list                    = dm.List
	remove                  = dm.Remove

	osExit           = os.Exit
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
)

func main() { osExit(run(os.Args)) }

// usageErr is the sentinel a sub-command returns to ask run to print usage and
// exit 2; check returns errExit to ask for exit 1.
const (
	exitOK    = 0
	exitErr   = 1
	exitUsage = 2
)

// run dispatches one dmtest invocation and returns its process exit code. It is
// pure with respect to its seams, so tests drive every branch without a kernel.
func run(args []string) int {
	if len(args) < 2 {
		return usage()
	}
	// need(n) reports whether args has at least n elements; on shortfall the
	// caller returns usage().
	need := func(n int) bool { return len(args) >= n }

	switch args[1] {
	case "version":
		v, err := version()
		if rc, bad := check(err); bad {
			return rc
		}
		fmt.Fprintf(stdout, "device-mapper interface version: %s\n", v)

	case "create":
		if !need(3) {
			return usage()
		}
		uuid := ""
		if len(args) > 3 {
			uuid = args[3]
		}
		if rc, bad := check(create(args[2], uuid)); bad {
			return rc
		}
		fmt.Fprintf(stdout, "created %s\n", args[2])

	case "linear":
		if !need(5) {
			return usage()
		}
		sectors, err := strconv.ParseUint(args[4], 10, 64)
		if rc, bad := check(err); bad {
			return rc
		}
		name, backing := args[2], args[3]
		if rc, bad := check(loadTable(name, []dm.Target{dm.Linear(0, sectors, backing, 0)})); bad {
			return rc
		}
		if rc, bad := check(resume(name)); bad {
			return rc
		}
		fmt.Fprintf(stdout, "loaded+resumed %s: linear %s 0 (%d sectors)\n", name, backing, sectors)

	case "striped":
		if !need(7) {
			return usage()
		}
		chunk, rc, bad := mustUint(args[3])
		if bad {
			return rc
		}
		sectors, rc, bad := mustUint(args[4])
		if bad {
			return rc
		}
		rest := args[5:]
		if len(rest)%2 != 0 {
			return usage()
		}
		var devs []dm.StripeDev
		for i := 0; i < len(rest); i += 2 {
			off, rc, bad := mustUint(rest[i+1])
			if bad {
				return rc
			}
			devs = append(devs, dm.StripeDev{Dev: rest[i], Offset: off})
		}
		if rc, bad := check(createWithTable(args[2], []dm.Target{dm.Striped(0, sectors, chunk, devs)})); bad {
			return rc
		}
		fmt.Fprintf(stdout, "created+resumed %s: striped %d devs chunk %d (%d sectors)\n", args[2], len(devs), chunk, sectors)

	case "zero":
		if !need(4) {
			return usage()
		}
		sectors, rc, bad := mustUint(args[3])
		if bad {
			return rc
		}
		if rc, bad := check(createWithTable(args[2], []dm.Target{dm.Zero(0, sectors)})); bad {
			return rc
		}
		fmt.Fprintf(stdout, "created+resumed %s: zero (%d sectors)\n", args[2], sectors)

	case "error":
		if !need(4) {
			return usage()
		}
		sectors, rc, bad := mustUint(args[3])
		if bad {
			return rc
		}
		if rc, bad := check(createWithTable(args[2], []dm.Target{dm.Error(0, sectors)})); bad {
			return rc
		}
		fmt.Fprintf(stdout, "created+resumed %s: error (%d sectors)\n", args[2], sectors)

	case "snapshot":
		if !need(8) {
			return usage()
		}
		persistent := args[5] == "P" || args[5] == "p"
		chunk, rc, bad := mustUint(args[6])
		if bad {
			return rc
		}
		sectors, rc, bad := mustUint(args[7])
		if bad {
			return rc
		}
		tgt := dm.Snapshot(0, sectors, args[3], args[4], persistent, chunk)
		if rc, bad := check(createWithTable(args[2], []dm.Target{tgt})); bad {
			return rc
		}
		fmt.Fprintf(stdout, "created+resumed %s: %s\n", args[2], tgt.String())

	case "snaporigin":
		if !need(5) {
			return usage()
		}
		sectors, rc, bad := mustUint(args[4])
		if bad {
			return rc
		}
		tgt := dm.SnapshotOrigin(0, sectors, args[3])
		if rc, bad := check(createWithTable(args[2], []dm.Target{tgt})); bad {
			return rc
		}
		fmt.Fprintf(stdout, "created+resumed %s: %s\n", args[2], tgt.String())

	case "snapstatus":
		if !need(3) {
			return usage()
		}
		s, err := snapshotStatus(args[2])
		if rc, bad := check(err); bad {
			return rc
		}
		fmt.Fprintf(stdout, "valid=%v used=%d total=%d metadata=%d raw=%q\n",
			s.Valid, s.UsedSectors, s.TotalSectors, s.MetadataSectors, s.Raw)

	case "crypt":
		if !need(9) {
			return usage()
		}
		key, err := hex.DecodeString(args[4])
		if rc, bad := check(err); bad {
			return rc
		}
		iv, rc, bad := mustUint(args[5])
		if bad {
			return rc
		}
		dev := args[6]
		off, rc, bad := mustUint(args[7])
		if bad {
			return rc
		}
		sectors, rc, bad := mustUint(args[8])
		if bad {
			return rc
		}
		var opts []string
		if len(args) > 9 {
			opts = args[9:]
		}
		tgt := dm.Crypt(0, sectors, args[3], key, iv, dev, off, opts)
		if rc, bad := check(createWithTable(args[2], []dm.Target{tgt})); bad {
			return rc
		}
		fmt.Fprintf(stdout, "created+resumed %s: crypt %s ... %s %d (%d sectors)\n", args[2], args[3], dev, off, sectors)

	case "thinpool":
		if !need(8) {
			return usage()
		}
		meta, data := args[3], args[4]
		dbs, rc, bad := mustUint(args[5])
		if bad {
			return rc
		}
		low, rc, bad := mustUint(args[6])
		if bad {
			return rc
		}
		sectors, rc, bad := mustUint(args[7])
		if bad {
			return rc
		}
		var opts []string
		if len(args) > 8 {
			opts = args[8:]
		}
		tgt := dm.ThinPool(0, sectors, meta, data, dbs, low, opts)
		if rc, bad := check(createWithTable(args[2], []dm.Target{tgt})); bad {
			return rc
		}
		fmt.Fprintf(stdout, "created+resumed %s: %s\n", args[2], tgt.String())

	case "createthin":
		if !need(4) {
			return usage()
		}
		id, rc, bad := mustUint(args[3])
		if bad {
			return rc
		}
		if rc, bad := check(thinPoolCreateThin(args[2], id)); bad {
			return rc
		}
		fmt.Fprintf(stdout, "created thin id %s in pool %s\n", args[3], args[2])

	case "createsnap":
		if !need(5) {
			return usage()
		}
		id, rc, bad := mustUint(args[3])
		if bad {
			return rc
		}
		origin, rc, bad := mustUint(args[4])
		if bad {
			return rc
		}
		if rc, bad := check(thinPoolCreateSnap(args[2], id, origin)); bad {
			return rc
		}
		fmt.Fprintf(stdout, "created snap id %s of origin %s in pool %s\n", args[3], args[4], args[2])

	case "deletethin":
		if !need(4) {
			return usage()
		}
		id, rc, bad := mustUint(args[3])
		if bad {
			return rc
		}
		if rc, bad := check(thinPoolDeleteThin(args[2], id)); bad {
			return rc
		}
		fmt.Fprintf(stdout, "deleted thin id %s from pool %s\n", args[3], args[2])

	case "thin":
		if !need(6) {
			return usage()
		}
		pool := args[3]
		devid, rc, bad := mustUint(args[4])
		if bad {
			return rc
		}
		sectors, rc, bad := mustUint(args[5])
		if bad {
			return rc
		}
		ext := ""
		if len(args) > 6 {
			ext = args[6]
		}
		tgt := dm.Thin(0, sectors, pool, devid, ext)
		if rc, bad := check(createWithTable(args[2], []dm.Target{tgt})); bad {
			return rc
		}
		fmt.Fprintf(stdout, "created+resumed %s: %s\n", args[2], tgt.String())

	case "thinpoolstatus":
		if !need(3) {
			return usage()
		}
		s, err := thinPoolStatus(args[2])
		if rc, bad := check(err); bad {
			return rc
		}
		fmt.Fprintf(stdout, "ok=%v txn=%d meta=%d/%d data=%d/%d raw=%q\n",
			s.OK, s.TransactionID, s.UsedMetaBlocks, s.TotalMetaBlocks,
			s.UsedDataBlocks, s.TotalDataBlocks, s.Raw)

	case "thinstatus":
		if !need(3) {
			return usage()
		}
		s, err := thinStatus(args[2])
		if rc, bad := check(err); bad {
			return rc
		}
		fmt.Fprintf(stdout, "ok=%v mapped=%d highest=%d hashighest=%v raw=%q\n",
			s.OK, s.MappedSectors, s.HighestMappedSector, s.HasHighest, s.Raw)

	case "message":
		if !need(4) {
			return usage()
		}
		sector, rc, bad := mustUint(args[3])
		if bad {
			return rc
		}
		msg := strings.Join(args[4:], " ")
		reply, err := message(args[2], sector, msg)
		if rc, bad := check(err); bad {
			return rc
		}
		fmt.Fprintf(stdout, "message %q -> reply %q\n", msg, reply)

	case "verity":
		if !need(14) {
			return usage()
		}
		ver, rc, bad := mustUint(args[3])
		if bad {
			return rc
		}
		dbs, rc, bad := mustUint(args[6])
		if bad {
			return rc
		}
		hbs, rc, bad := mustUint(args[7])
		if bad {
			return rc
		}
		ndb, rc, bad := mustUint(args[8])
		if bad {
			return rc
		}
		hsb, rc, bad := mustUint(args[9])
		if bad {
			return rc
		}
		p := dm.VerityParams{
			Version:        ver,
			DataDev:        args[4],
			HashDev:        args[5],
			DataBlockSize:  dbs,
			HashBlockSize:  hbs,
			NumDataBlocks:  ndb,
			HashStartBlock: hsb,
			Algorithm:      args[10],
			RootDigest:     args[11],
			Salt:           args[12],
		}
		sectors, rc, bad := mustUint(args[13])
		if bad {
			return rc
		}
		if len(args) > 14 {
			p.Opts = args[14:]
		}
		tgt := dm.Verity(0, sectors, p)
		if rc, bad := check(createReadOnlyWithTable(args[2], []dm.Target{tgt})); bad {
			return rc
		}
		fmt.Fprintf(stdout, "created+resumed %s: %s\n", args[2], tgt.String())

	case "status":
		if !need(3) {
			return usage()
		}
		tgts, err := status(args[2])
		if rc, bad := check(err); bad {
			return rc
		}
		for _, t := range tgts {
			fmt.Fprintln(stdout, t.String())
		}

	case "info":
		if !need(3) {
			return usage()
		}
		i, err := info(args[2])
		if rc, bad := check(err); bad {
			return rc
		}
		fmt.Fprintf(stdout, "name=%s dev=%d:%d open=%d targets=%d flags=%#x suspended=%v active=%v\n",
			i.Name, i.Major(), i.Minor(), i.OpenCount, i.TargetCnt, i.Flags, i.Suspended(), i.ActivePresent())

	case "table":
		if !need(3) {
			return usage()
		}
		tbl, err := tableStatus(args[2])
		if rc, bad := check(err); bad {
			return rc
		}
		for _, t := range tbl {
			fmt.Fprintln(stdout, t.String())
		}

	case "list":
		devs, err := list()
		if rc, bad := check(err); bad {
			return rc
		}
		for _, d := range devs {
			fmt.Fprintf(stdout, "%s (dev %d)\n", d.Name, d.Dev)
		}

	case "remove":
		if !need(3) {
			return usage()
		}
		if rc, bad := check(remove(args[2])); bad {
			return rc
		}
		fmt.Fprintf(stdout, "removed %s\n", args[2])

	default:
		return usage()
	}
	return exitOK
}

// usage prints the synopsis and returns the usage exit code.
func usage() int {
	fmt.Fprintln(stderr, "usage: dmtest <version|create|linear|striped|zero|error|snapshot|snaporigin|snapstatus|crypt|thinpool|createthin|createsnap|deletethin|thin|thinpoolstatus|thinstatus|message|verity|info|table|status|list|remove> ...")
	return exitUsage
}

// mustUint parses s as a uint64. On failure it prints the error and returns the
// error exit code with bad=true so the caller can return immediately.
func mustUint(s string) (v uint64, rc int, bad bool) {
	v, err := strconv.ParseUint(s, 10, 64)
	if rc, bad := check(err); bad {
		return 0, rc, bad
	}
	return v, exitOK, false
}

// check reports an error to stderr and signals (exitErr, true) when err != nil,
// or (exitOK, false) otherwise.
func check(err error) (rc int, bad bool) {
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return exitErr, true
	}
	return exitOK, false
}
