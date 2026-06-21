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
//	dmtest info    <name>
//	dmtest table   <name>
//	dmtest list
//	dmtest remove  <name>
package main

import (
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
	fmt.Fprintln(os.Stderr, "usage: dmtest <version|create|linear|info|table|list|remove> ...")
	os.Exit(2)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
