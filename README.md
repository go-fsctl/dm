# go-fsctl/dm

Pure-Go control of the Linux **device-mapper** subsystem, driven directly
through the `/dev/mapper/control` character device and the `DM_*` ioctls.

- **No cgo.** Just the standard library and `golang.org/x/sys/unix`.
- **No `dmsetup`.** The library speaks the `struct dm_ioctl` wire protocol
  itself; it never shells out.

`dm` is the device-mapper member of the **go-fsctl** family of pure-Go kernel
control libraries:

| Module | Kernel surface |
| ------ | -------------- |
| [`go-fsctl/zfs`](https://github.com/go-fsctl/zfs)     | ZFS `/dev/zfs` ioctls |
| [`go-fsctl/btrfs`](https://github.com/go-fsctl/btrfs) | Btrfs `BTRFS_IOC_*` ioctls |
| [`go-fsctl/loop`](https://github.com/go-fsctl/loop)   | Loop devices via `/dev/loop-control` + `LOOP_*` |
| **`go-fsctl/dm`** | **device-mapper via `/dev/mapper/control` + `DM_*`** |

## Install

```sh
go get github.com/go-fsctl/dm
```

## Usage

A device-mapper device is built in three steps that mirror the kernel's
active/inactive table model, then torn down with `Remove`:

```go
package main

import "github.com/go-fsctl/dm"

func main() {
	// 1. Create an empty, suspended device.
	if err := dm.Create("myvol", ""); err != nil {
		panic(err)
	}
	// 2. Load a table into the inactive slot. A "linear" target maps a run of
	//    sectors onto a backing device at a sector offset.
	//    Here: map 131072 sectors (64 MiB) of /dev/loop0, starting at offset 0.
	tgt := dm.Linear(0, 131072, "/dev/loop0", 0) // == Target{Type:"linear", Params:"/dev/loop0 0"}
	if err := dm.LoadTable("myvol", []dm.Target{tgt}); err != nil {
		panic(err)
	}
	// 3. Resume: promote the inactive table to active and enable IO.
	if err := dm.Resume("myvol"); err != nil {
		panic(err)
	}
	// /dev/mapper/myvol is now live.

	// ... use it ...

	if err := dm.Remove("myvol"); err != nil {
		panic(err)
	}
}
```

`Target` is intentionally generic, so any kernel target works by supplying the
right `Type` and `Params`. For the common targets there are typed constructors
that emit the exact param string the kernel expects, so you do not have to
remember the grammar:

```go
dm.Linear(0, n, "/dev/loop0", 0)                      // linear: <dev> <offset>
dm.Striped(0, n, 256, []dm.StripeDev{                 // striped: <num> <chunk> (<dev> <off>)...
	{Dev: "/dev/loop0", Offset: 0},
	{Dev: "/dev/loop1", Offset: 0},
})                                                    //   => "2 256 /dev/loop0 0 /dev/loop1 0"
dm.Zero(0, n)                                         // zero:  reads return zeros, writes dropped
dm.Error(0, n)                                        // error: all I/O fails
dm.Snapshot(0, n, "/dev/origin", "/dev/cow", true, 8) // snapshot: <origin> <cow> <P|N> <chunk>
dm.SnapshotOrigin(0, n, "/dev/origin")                // snapshot-origin: <origin>
dm.Crypt(0, n, "aes-xts-plain64", key, 0, "/dev/loop0", 0, nil)
	// crypt: <cipher> <hexkey> <iv> <dev> <off> [<#opt> <opt>...]
	// key is raw bytes; Crypt hex-encodes it as the dm-crypt table format requires.
dm.ThinPool(0, n, "/dev/meta", "/dev/data", 128, 1024, nil)
	// thin-pool: <meta> <data> <data_block_sectors> <low_water_blocks> [<#opt> <opt>...]
dm.Thin(0, n, "/dev/mapper/pool", 0, "")
	// thin: <pool> <dev_id> [<external_origin>]
dm.Verity(0, n, dm.VerityParams{Version: 1, DataDev: "/dev/data", HashDev: "/dev/hash",
	DataBlockSize: 4096, HashBlockSize: 4096, NumDataBlocks: n/8, HashStartBlock: 1,
	Algorithm: "sha256", RootDigest: root, Salt: salt})
	// verity: <ver> <data> <hash> <dbs> <hbs> <ndb> <hsb> <algo> <root> <salt> [<#opt> <opt>...]
```

Each constructor only assembles the table line — the kernel does the striping,
copy-on-write and crypto. `CreateWithTable(name, targets)` is a one-shot
convenience that does `Create` + `LoadTable` + `Resume` (and cleans up on
failure):

```go
err := dm.CreateWithTable("crypt0", []dm.Target{
	dm.Crypt(0, 131072, "aes-xts-plain64", key, 0, "/dev/loop0", 0, nil),
})
```

`SnapshotStatus(name)` reads a snapshot device's runtime status and decodes the
`used/total` exception-store usage (and the metadata-sectors word) into a
`SnapStatus`; `ParseSnapStatus` is exposed separately for parsing a status
string directly.

### Thin provisioning

A thin pool hands out data blocks on demand to thin volumes and snapshots, all
identified by numeric device ids. The pool's metadata device must be **zeroed**
before first use (the kernel treats zeroed metadata as "format me"). Thin
volumes and snapshots are created *inside* the pool by sending it target
messages (the `DM_TARGET_MSG` ioctl), not by reloading its table:

```go
// 1. Pool over a zeroed metadata device and a data device, 64 KiB blocks.
dm.CreateWithTable("pool", []dm.Target{
	dm.ThinPool(0, dataSectors, "/dev/meta", "/dev/data", 128, 1024, nil),
})
// 2. Create thin volume id 0 inside the pool (Message: "create_thin 0").
dm.ThinPoolCreateThin("pool", 0)
// 3. Surface it as a block device, then format/mount /dev/mapper/vol0.
dm.CreateWithTable("vol0", []dm.Target{dm.Thin(0, volSectors, "/dev/mapper/pool", 0, "")})
// 4. Snapshot id 0 as id 1 (Message: "create_snap 1 0"); surface as another thin.
dm.ThinPoolCreateSnap("pool", 1, 0)
dm.CreateWithTable("snap0", []dm.Target{dm.Thin(0, volSectors, "/dev/mapper/pool", 1, "")})
// 5. dm.ThinPoolDeleteThin("pool", 1) removes a thin/snap by id.
```

`Message(name, sector, msg)` is the general `DM_TARGET_MSG` wrapper (sector is
usually 0 to address a single-row table); the `ThinPool*` helpers above are thin
wrappers over it. `ThinPoolStatus(name)` / `ThinStatus(name)` decode the pool's
`used/total` data and metadata blocks and a thin volume's mapped sectors.

### dm-verity

`Verity(VerityParams)` builds a read-only integrity-checked mapping over a data
device and a precomputed Merkle hash tree (e.g. from `veritysetup format`). The
device **must be created read-only**, so use `CreateReadOnlyWithTable` (or
`LoadTableReadOnly`):

```go
dm.CreateReadOnlyWithTable("verity0", []dm.Target{
	dm.Verity(0, dataSectors, dm.VerityParams{
		Version: 1, DataDev: "/dev/data", HashDev: "/dev/hash",
		DataBlockSize: 4096, HashBlockSize: 4096,
		NumDataBlocks: nBlocks, HashStartBlock: 1, // block 0 holds veritysetup's superblock
		Algorithm: "sha256", RootDigest: rootHex, Salt: saltHex,
	}),
})
```

Every block read is verified against the root digest; a mismatch returns EIO and
flips `ParseVerityStatus` from `"V"` to `"C"`. Optional `Opts` (e.g.
`ignore_zero_blocks`) and forward error correction (`VerityFEC`) are folded into
the table's optional-argument list.

## API

| Function | ioctl | Purpose |
| -------- | ----- | ------- |
| `Available() bool`                       | —                | `/dev/mapper/control` openable |
| `Version() (DMVersion, error)`           | `DM_VERSION`      | negotiate interface version |
| `Create(name, uuid string) error`        | `DM_DEV_CREATE`   | create empty suspended device |
| `CreateWithTable(name, []Target) error`  | create+load+resume | one-shot bring-up (cleans up on failure) |
| `CreateReadOnlyWithTable(name, []Target) error` | create+load+resume | as above but read-only (dm-verity) |
| `LoadTable(name, []Target) error`        | `DM_TABLE_LOAD`   | load table into inactive slot |
| `LoadTableReadOnly(name, []Target) error` | `DM_TABLE_LOAD`  | load read-only table (dm-verity) |
| `Message(name, sector, msg) (string, error)` | `DM_TARGET_MSG` | send a target message (thin create/delete/snap) |
| `Suspend(name string) error`             | `DM_DEV_SUSPEND`  | suspend IO (`DM_SUSPEND_FLAG` set) |
| `Resume(name string) error`              | `DM_DEV_SUSPEND`  | resume / activate (flag clear) |
| `Info(name string) (DevInfo, error)`     | `DM_DEV_STATUS`   | dev_t, open count, flags, target count |
| `TableStatus(name string) ([]Target, _)` | `DM_TABLE_STATUS` | read back the active table |
| `Status(name string) ([]Target, _)`      | `DM_TABLE_STATUS` | read back per-target runtime status |
| `List() ([]Device, error)`               | `DM_LIST_DEVICES` | enumerate all dm devices |
| `Remove(name string) error`              | `DM_DEV_REMOVE`   | remove device, destroy tables |

`Linear`, `Striped`, `Zero`, `Error`, `Snapshot`, `SnapshotOrigin`, `Crypt`,
`ThinPool`, `Thin` and `Verity` are convenience constructors for the
corresponding kernel targets; they build a `Target` value and so work on every
platform. `SnapshotStatus`, `ThinPoolStatus`, `ThinStatus` (and the matching
`Parse*Status` functions) decode the targets' runtime status; the `ThinPool*`
helpers wrap `Message` for thin/snapshot lifecycle.

On non-Linux platforms every kernel operation returns `ErrUnsupported`, while
the ABI definitions and the target-spec (de)serialization in `abi.go` remain
available for tooling and tests.

## How it works

Every device-mapper command sends a single contiguous buffer beginning with a
`struct dm_ioctl` header, optionally followed by a payload:

- `DM_TABLE_LOAD` appends a sequence of `struct dm_target_spec` records, each
  followed by its NUL-terminated parameter string and padded to an 8-byte
  boundary; each spec's `next` field is the byte distance to the following one.
- `DM_TABLE_STATUS` / `DM_LIST_DEVICES` return variable-length payloads. The
  library sizes the buffer, and if the kernel sets `DM_BUFFER_FULL_FLAG` it
  doubles the buffer and retries.

The `data_size` (total buffer length) and `data_start` (offset to the payload,
i.e. `sizeof(struct dm_ioctl)`) bookkeeping fields are set on every call. The
`DM_*` request numbers are *derived* in Go from the `_IOWR(0xfd, cmd,
sizeof(struct dm_ioctl))` encoding rather than hard-coded, and pinned by unit
tests.

## Testing

Unit tests (ioctl numbers, struct sizes/offsets, target-spec serialization)
run on any host:

```sh
GOWORK=off go test ./...
```

Integration tests touch the real subsystem and are gated on
`/dev/mapper/control` plus root; they self-skip otherwise:

```sh
sudo -E go test -run Live -v ./...   # or run the full suite as root
```

## License

BSD-3-Clause. See [LICENSE](LICENSE).
