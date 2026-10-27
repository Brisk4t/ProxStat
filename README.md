# StatMonitorEPD

Shows a host and up to 12 containers or VMs on a 1.54" 152×152 4-color e-paper panel (black, white, yellow, red). Data arrives over USB serial. Only the parts of the screen that changed get redrawn.

- One header row (the host) and 1–4 list items per page (`ITEMS_PER_PAGE`, default 4), up to 12 items. With fewer items per page, each card is taller.
- A button steps to the next page and wraps around after the last one.
- Each item shows a name, an IP, disk and RAM usage, and a usage bar. Offline items are drawn with diagonal lines.
- Each change marks only its section stale (name, IP, disk, RAM or bar). All stale sections are drawn in one partial refresh.
- Serial input keeps being read while the panel refreshes (12–25 s), so the host never blocks.
- A full refresh runs every 12 hours to clear ghosting and prevent burn-in.

Supported boards: STM32F103 Blue Pill, ESP32-C3 SuperMini, ESP32-S3. The panel driver is in [lib/GxEPD2_154c_152](lib/GxEPD2_154c_152).

## Building

```sh
pio run -e stm32f103 -t upload          # Blue Pill (DFU)
pio run -e esp32c3_supermini -t upload
pio run -e esp32s3_n16r8 -t upload
```

## Wiring

| Signal | Blue Pill | ESP32-C3 | ESP32-S3 |
|---|---|---|---|
| SCK | PA5 | 10 | 5 |
| MOSI (SDA) | PA7 | 7 | 6 |
| CS | PB1 | 6 | 15 |
| DC | PB0 | 21 | 16 |
| RST | PA3 | 20 | 7 |
| BUSY | PB10 | 5 | 4 |
| Page button (to GND) | PB11 | 9 (BOOT) | 0 (BOOT) |
| LED (lit while refreshing) | PC13 | none | none |

Pins are set per board in [platformio.ini](platformio.ini). On the Blue Pill, SCK and MOSI must be SPI1 pins. Keep clear of PA11/PA12 (USB), PA13/PA14 (SWD) and PB2 (BOOT1).

## Build flags

These go in the `[epd]` section of [platformio.ini](platformio.ini), which every board uses.

| Flag | Default | Meaning |
|---|---|---|
| `EPD_FRAME_RATE` | `0` | Panel frame rate in Hz: 12 (12.5), 25, 50, 65, 75, 85, 100 or 120. The nearest supported rate is used. `0` uses the vendor default. |
| `EPD_TEMPERATURE` | `93` | Temperature in °C forced on the controller, which uses it to choose a waveform. 93 selects the fastest one. Lower values are slower but give more contrast. |
| `EPD_ROTATION` | `0` | Rotation at boot, in 90° steps clockwise: `0`, `1`, `2`, or `-1` for 270°. The `rot=N` serial command changes it until the next reset. |
| `ITEMS_PER_PAGE` | `4` | List items per page, 1–4. Cards share the space below the header evenly: 27, 37, 57 or 117 px tall. The text rows spread out and the usage bar thickens (2, 2, 4, 8 px), but the font stays the same size. |
| `EPD_ABORT_MS` | `0` | Cuts partial refreshes short after this many ms, for faster but less clean updates. `0` = off. Full refreshes always finish. |
| `EPD_PAGE_HEIGHT` | `38` on STM32, `152` elsewhere | Rows drawn per pass. 152 = whole frame (5.8 KB buffer). 38 = 4 passes (1.4 KB). The board has only been tested with `152`. |
| `EPD_SPI_HZ` | `10000000` | SPI clock. 4 MHz hung the Blue Pill on its first command after reset, so leave this at 10 MHz unless you have a reason to change it. |
| `FULL_REFRESH_HOURS` | `12` | Interval between anti-burn-in full refreshes. Fractions are allowed, e.g. `0.1` for testing. |
| `UPDATE_HOLDOFF_MS` | `300` | How long the first stale change waits before a partial refresh starts, so a burst of lines ends up in a single refresh. |
| `BAR_WARN_PCT` / `BAR_CRIT_PCT` | `50` / `80` | Bar color thresholds. Below warn: black (white in the header). From warn: yellow. From crit: red. |
| `VALUE_UNIT` | `"GB"` | Unit added after disk and RAM values. Write it as `-DVALUE_UNIT=\"GB\"`. `\"\"` = none. |
| `EPD_DEBUG` | off | Prints boot and render progress, a busy heartbeat and each refresh's duration on Serial. |
| `BUTTON_PIN`, `LED_PIN`, `LED_ON_LEVEL`, `SERIAL_BAUD` | per board | Board I/O. `-1` disables the button or the LED. |

## Serial protocol

Commands are plain ASCII lines ending in `\n` or `\r\n`, at most 127 characters each.

### Replies

Every non-empty line gets exactly one reply, in the order the lines were sent:

| Reply | Meaning |
|---|---|
| `OK` | The line was applied. The screen may not show it yet, because refreshes are batched and take seconds. |
| `ERR <reason>` | The line was rejected, or one of its fields was. Good fields on the same line are still applied, and the reply gives the first error. |
| `VER ...` | The reply to `ver` (see [Commands](#commands)). |

Reasons: `unknown command`, `unknown field`, `field without '='`, `item must be H or 1..12`, `no such page`, `rotation must be -1..3`, `line too long`.

With `EPD_DEBUG` on, debug lines are mixed in with the replies. Debug lines never start with `OK`, `ERR ` or `VER `, so a host can skip anything else. Because the replies are in order, the host can match them to lines by counting, without sequence numbers.

### Updating a row

```
<target>|<key>=<value>|<key>=<value>...
```

`<target>` is `H` for the header, or `1`–`12` for a list item. Items fill pages in order: with the default 4 per page, items `1`–`4` are page 1, `5`–`8` page 2 and `9`–`12` page 3. Send any subset of fields. A field whose value hasn't changed doesn't trigger a refresh.

| Key | Value | Example | Shown as |
|---|---|---|---|
| `n` | name, up to 15 characters (yellow in the header) | `n=Container 1` | first line, left |
| `ip` | second line, up to 15 characters | `ip=192.168.1.123` | second line, left |
| `d` | disk `used/total`, up to 11 characters | `d=12/256` | `12/256 GB` next to the drive icon |
| `r` | RAM `used/total`, up to 11 characters | `r=3.1/8` | `3.1/8 GB` next to the RAM icon |
| `b` | bar, 0–100 % | `b=72` | bar along the bottom, colored by the thresholds |
| `on` | `1` online, `0` offline (list items only) | `on=0` | diagonal lines across the card |

If a value is too wide, the unit is shortened in steps: `820/2000 GB`, then `820/2000GB`, then `820/2000G`, then just the number.

Each field redraws only its own section of the screen. Changing `on`, or setting a field on an item for the first time, redraws the whole card.

### Commands

| Line | Effect |
|---|---|
| `3\|del` | Remove item 3. `H\|del` resets the header. |
| `clear` | Remove all items. |
| `next` | Next page, same as pressing the button. |
| `page=2` | Jump to page 2. |
| `rot=1` | Rotate the drawing: `0`, `1`, `2`, `3`, or `-1` (same as `3`), in 90° steps clockwise. Triggers a full refresh. Not saved: after a reset, `EPD_ROTATION` applies again. |
| `full` | Run a full refresh now. |
| `ver` | Reply with the protocol version and limits, e.g. `VER 1 items=12 per_page=4 n=15 ip=15 d=11 r=11 line=127`: the item count, items per page, maximum length of each field, and maximum line length. The version goes up whenever the protocol changes in a way that isn't backward compatible. |

### Example

```
H|n=Proxmox:|ip=192.168.1.254|d=412/931|r=27/64|b=86
1|n=Container 1|ip=192.168.1.123|d=12/256|r=3.1/8|b=91|on=1
4|n=Container 4|ip=192.168.1.126|d=20/64|r=0/8|b=0|on=0
2|b=55
```

## Tests

[tests/](tests) holds two scripts that send mock data: [send-mock.ps1](tests/send-mock.ps1) for Windows PowerShell and [send-mock.sh](tests/send-mock.sh) for bash or Git Bash. With no test mode, they send [mock-data.txt](tests/mock-data.txt): a header, 4 items on page 1 (one of them offline), and 2 items on page 2 that test the longest IP and value shortening.

Close any serial monitor on the port first, or the scripts can't open it.

| Test | Sends | Expected refresh |
|---|---|---|
| `mock` (default) | the whole mock file | everything |
| `item` | one whole list item, every field | that card |
| `name`, `ip` | that field only | the left text area |
| `disk`, `ram` | that field only | the icon and value area |
| `bar` | the usage bar only | the strip at the bottom of the card |
| `online` | the online flag | the whole card |
| `fields` | name, ip, disk, ram and bar as separate lines | one per field, or one refresh if they arrive close together |

Every test except `mock` uses random values, so each send is a real change.

### PowerShell

```powershell
.\tests\send-mock.ps1                                        # COM10, the mock file
.\tests\send-mock.ps1 -Port COM5 -File tests\other.txt
.\tests\send-mock.ps1 -Test item -Item 2                     # whole item 2
.\tests\send-mock.ps1 -Test bar -Item 2                      # only item 2's usage bar
.\tests\send-mock.ps1 -Test disk -Item H                     # only the header's disk value
.\tests\send-mock.ps1 -Test fields -Item 3 -IntervalMs 30000 # each field in its own refresh
.\tests\send-mock.ps1 -Test bar -Count 20 -IntervalMs 100    # rapid updates, should give one refresh
.\tests\send-mock.ps1 -Line '4|on=1','next'                  # literal lines
.\tests\send-mock.ps1 -Line 'rot=1'                          # rotate 90 degrees
.\tests\send-mock.ps1 -Line 'ver'                            # protocol version and limits
```

Parameters: `-Port` (default `COM10`), `-File`, `-Test`, `-Item` (`1`–`12` or `H`, default `1`), `-Count`, `-IntervalMs`, `-Line`, `-Baud`, `-ReplyMs`.

### Bash

```bash
tests/send-mock.sh -p /dev/ttyACM0                 # Linux, the mock file
tests/send-mock.sh -p COM10                        # Git Bash: COMn becomes /dev/ttyS<n-1>
tests/send-mock.sh -p COM10 -t item -i 2
tests/send-mock.sh -p COM10 -t bar -i 2
tests/send-mock.sh -p COM10 -t disk -i H
tests/send-mock.sh -p COM10 -t fields -i 3 -d 30000
tests/send-mock.sh -p COM10 -t bar -n 20 -d 100
tests/send-mock.sh -p COM10 '4|on=1' 'next'        # literal lines
tests/send-mock.sh -p COM10 'rot=1'                # rotate 90 degrees
tests/send-mock.sh -p COM10 'ver'                  # protocol version and limits
```

Options: `-p` port (default `/dev/ttyACM0`), `-f` file, `-t` test, `-i` item, `-n` count, `-d` interval in ms, `-b` baud.

Both scripts print every reply, then a summary such as `Sent 7 line(s) to COM10, 7 acked, 0 error(s)`. If the number of acks doesn't match the number of lines sent, or any line got an error, the PowerShell script prints the summary as a warning and the bash script exits with status 1. The bash script only reads replies after its last line, then waits half a second, so with long `-d` intervals the counts are only checked at the end.

With `EPD_DEBUG` on, each refresh also prints `render partial x,y wxh` (the refreshed window) and `render done in N ms`.
