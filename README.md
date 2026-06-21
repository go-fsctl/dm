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
right `Type` and `Params`:

```go
dm.Target{SectorStart: 0, Length: n, Type: "striped",  Params: "2 256 /dev/sda 0 /dev/sdb 0"}
dm.Target{SectorStart: 0, Length: n, Type: "snapshot", Params: "/dev/origin /dev/cow P 8"}
dm.Target{SectorStart: 0, Length: n, Type: "crypt",    Params: "aes-xts-plain64 <key> 0 /dev/loop0 0"}
```

## API

| Function | ioctl | Purpose |
| -------- | ----- | ------- |
| `Available() bool`                       | —                | `/dev/mapper/control` openable |
| `Version() (DMVersion, error)`           | `DM_VERSION`      | negotiate interface version |
| `Create(name, uuid string) error`        | `DM_DEV_CREATE`   | create empty suspended device |
| `LoadTable(name, []Target) error`        | `DM_TABLE_LOAD`   | load table into inactive slot |
| `Suspend(name string) error`             | `DM_DEV_SUSPEND`  | suspend IO (`DM_SUSPEND_FLAG` set) |
| `Resume(name string) error`              | `DM_DEV_SUSPEND`  | resume / activate (flag clear) |
| `Info(name string) (DevInfo, error)`     | `DM_DEV_STATUS`   | dev_t, open count, flags, target count |
| `TableStatus(name string) ([]Target, _)` | `DM_TABLE_STATUS` | read back the active table |
| `Status(name string) ([]Target, _)`      | `DM_TABLE_STATUS` | read back per-target runtime status |
| `List() ([]Device, error)`               | `DM_LIST_DEVICES` | enumerate all dm devices |
| `Remove(name string) error`              | `DM_DEV_REMOVE`   | remove device, destroy tables |

`Linear(start, length, dev, devOffset)` is a convenience constructor for the
`"linear"` target.

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
