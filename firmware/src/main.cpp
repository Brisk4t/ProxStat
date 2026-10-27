// StatMonitorEPD: host + container status list on a 1.54" 152x152 4-color e-paper
// (black/white/yellow/red, GxEPD2_154c_152 driver in /lib).
//
// Layout: a header row (the host) and ITEMS_PER_PAGE cards per page (1..4, default 4), 12 cards max.
// Fewer cards per page make each card taller. A button on BUTTON_PIN (active low) steps to the
// next page and wraps around.
//
// Serial protocol: one command per line ('\n' or "\r\n"), max 127 chars.
//   H|n=Proxmox:|ip=192.168.1.254|d=120/512|r=12/64|b=72        header row
//   3|n=Container 3|ip=192.168.1.123|d=8/32|r=1.5/4|b=20|on=1   list item 1..12
//   3|b=55                  any subset of fields can be sent; only changed fields are redrawn
//   3|del                   remove item 3 (H|del resets the header)
//   clear                   remove all items
//   next                    same as a button press
//   page=2                  jump to page 2
//   rot=1                   draw rotation in 90 degree steps: 0, 1, 2, 3 or -1 (= 3); full refresh.
//                           Not saved, EPD_ROTATION applies again after a reset
//   full                    full refresh now
//   ver                     protocol version and limits:
//                           "VER 1 items=12 per_page=4 n=15 ip=15 d=11 r=11 line=127"
// Fields:
//   n   name (header: title, drawn in yellow)
//   ip  second text line
//   d   disk "used/total", drawn as "<d> GB" (unit dropped/shortened when it doesn't fit)
//   r   ram "used/total", same format
//   b   bar 0..100 %: black/white below BAR_WARN_PCT, yellow below BAR_CRIT_PCT, red above
//   on  0 = offline (diagonal lines across the card, broken around the text), 1 = online
// Every non-empty line gets exactly one reply, in order: "OK", "ERR <reason>" or (for ver) the VER
// line. OK means the line was applied to the model, not that it is on screen yet. A line with a bad
// field still applies its good fields and replies with the first error.
//
// Updates: every change marks its section (name, ip, disk, ram, bar or whole card) stale. Once
// stale data has waited UPDATE_HOLDOFF_MS, the bounding box of all stale sections is redrawn
// with one partial refresh. Serial and the button are serviced from the driver's busy callback,
// so data keeps flowing while the panel refreshes; drawing works from a snapshot so changes
// arriving mid-refresh just mark their sections stale again for the next pass.
// Every FULL_REFRESH_HOURS the whole screen gets a full refresh to clear ghosting / burn-in.
//
// Build flags (all optional except the pins):
//   EPD_FRAME_RATE      panel frame rate in Hz: 12 (12.5), 25, 50, 65, 75, 85, 100, 120 (nearest
//                       is used), 0 = vendor default
//   EPD_TEMPERATURE     forced temperature in degC the controller picks its waveform with,
//                       driver default 93 (fastest band)
//   EPD_ABORT_MS        cut partial refreshes short after this many ms, 0 = off (see driver)
//   EPD_PAGE_HEIGHT     rows per drawing page (default 38 on STM32 = 1444 B, full frame elsewhere)
//   EPD_SPI_HZ          SPI clock (default 10 MHz)
//   EPD_ROTATION        rotation at boot in 90 degree steps clockwise: 0, 1, 2, -1 (= 270); "rot=" changes it
//   ITEMS_PER_PAGE      cards per page, 1..4 (default 4); card height scales to fill the screen
//   FULL_REFRESH_HOURS  full refresh interval (default 12, fractions allowed)
//   UPDATE_HOLDOFF_MS   batch window before a partial refresh (default 300)
//   BAR_WARN_PCT, BAR_CRIT_PCT  bar color thresholds (default 50, 80)
//   VALUE_UNIT          unit appended to disk/ram values (default "GB", "" for none)
//   BUTTON_PIN, LED_PIN, LED_ON_LEVEL, SERIAL_BAUD
//   EPD_DEBUG           print boot / render progress and the driver's busy timings on Serial

#include <Arduino.h>
#include <SPI.h>
#include <GxEPD2_4C.h>
#include <GxEPD2_154c_152.h>

#if !defined(EPD_CS_PIN) || !defined(EPD_DC_PIN) || !defined(EPD_RST_PIN) || !defined(EPD_BUSY_PIN)
#error "define EPD_CS_PIN, EPD_DC_PIN, EPD_RST_PIN and EPD_BUSY_PIN in platformio.ini"
#endif

#ifndef EPD_FRAME_RATE
#define EPD_FRAME_RATE 0
#endif
#ifndef EPD_ABORT_MS
#define EPD_ABORT_MS 0
#endif
#ifndef EPD_PAGE_HEIGHT
#if defined(ARDUINO_ARCH_STM32)
#define EPD_PAGE_HEIGHT 38
#else
#define EPD_PAGE_HEIGHT 152
#endif
#endif
#ifndef EPD_SPI_HZ
#define EPD_SPI_HZ 10000000
#endif
#ifndef EPD_ROTATION
#define EPD_ROTATION 0
#endif
#ifndef ITEMS_PER_PAGE
#define ITEMS_PER_PAGE 4
#endif
#if (ITEMS_PER_PAGE < 1) || (ITEMS_PER_PAGE > 4)
#error "ITEMS_PER_PAGE must be 1..4"
#endif
#ifndef FULL_REFRESH_HOURS
#define FULL_REFRESH_HOURS 12
#endif
#ifndef UPDATE_HOLDOFF_MS
#define UPDATE_HOLDOFF_MS 300
#endif
#ifndef BAR_WARN_PCT
#define BAR_WARN_PCT 50
#endif
#ifndef BAR_CRIT_PCT
#define BAR_CRIT_PCT 80
#endif
#ifndef VALUE_UNIT
#define VALUE_UNIT "GB"
#endif
#ifndef BUTTON_PIN
#define BUTTON_PIN -1
#endif
#ifndef LED_PIN
#define LED_PIN -1
#endif
#ifndef LED_ON_LEVEL
#define LED_ON_LEVEL LOW
#endif
#ifndef SERIAL_BAUD
#define SERIAL_BAUD 115200
#endif

GxEPD2_4C<GxEPD2_154c_152, EPD_PAGE_HEIGHT> display(GxEPD2_154c_152(EPD_CS_PIN, EPD_DC_PIN, EPD_RST_PIN, EPD_BUSY_PIN));

static const int buttonPin = BUTTON_PIN;
static const int ledPin = LED_PIN;
static const uint32_t FULL_REFRESH_MS = uint32_t(FULL_REFRESH_HOURS * 3600000UL);
static const uint32_t BUTTON_DEBOUNCE_MS = 30;

// ---------------------------------------------------------------- layout (152x152)
// slot 0 is the header (30 rows), slots 1..ITEMS_PER_PAGE the cards, which split the rows down to
// the page indicator evenly (30, 40, 60 or 120 rows); every slot ends in a 3 row gap
static const int16_t SCREEN_W = 152;
static const int16_t HEADER_H = 30;
static const int16_t CARD_SLOT_H = ((SCREEN_W - HEADER_H - 2) / ITEMS_PER_PAGE) & ~1;
static const int16_t CARD_X = 1, CARD_W = 150, CARD_H = CARD_SLOT_H - 3, CARD_R = 3;
static const int16_t TEXT_X = 3, TEXT_W = 81;   // name / ip column, 3..83
static const int16_t ICON_X = 85;               // 12 px icons, 85..96
static const int16_t VALUE_X = 98, VALUE_W = 51; // 98..148
static const int16_t INDICATOR_Y = 148, INDICATOR_H = 2;

// vertical layout within a slot: two 8 px text rows and the usage bar below them
struct RowLayout { int16_t row1, row2, barY, barH; };
static const RowLayout HEADER_LAYOUT = { 3, 13, 23, 2 };
// cards: the bar thickens with the card (2, 2, 4, 8 px) and sits 2 rows above its bottom edge;
// the rows above the bar split into top margin, row gap and bottom margin (3, 2, 2 at 4 per page)
static const int16_t CARD_BAR_H = (CARD_H / 14 > 2) ? (CARD_H / 14) & ~1 : 2;
static const int16_t CARD_BAR_Y = CARD_H - 2 - CARD_BAR_H;
static const int16_t CARD_ROW_GAP = (CARD_BAR_Y - 16) / 3;
static const int16_t CARD_ROW1 = (CARD_BAR_Y - 16 - CARD_ROW_GAP + 1) / 2;
static const RowLayout CARD_LAYOUT = { CARD_ROW1, int16_t(CARD_ROW1 + 8 + CARD_ROW_GAP), CARD_BAR_Y, CARD_BAR_H };

// sections of a slot that can go stale independently
enum : uint8_t { SEC_NAME = 0x01, SEC_IP = 0x02, SEC_DISK = 0x04, SEC_RAM = 0x08, SEC_BAR = 0x10, SEC_CARD = 0x20 };
static const uint8_t SEC_COUNT = 6;
struct Rect { int16_t x, y, w, h; };
static const Rect INDICATOR_RECT = { 0, 146, 152, 6 };

static int16_t slotY(uint8_t slot)
{
  return slot ? HEADER_H + (slot - 1) * CARD_SLOT_H : 0;
}

static const RowLayout& layoutOf(uint8_t slot)
{
  return slot ? CARD_LAYOUT : HEADER_LAYOUT;
}

// section rect relative to the slot top, 1 px around the text rows / bar; render() aligns the
// union to the panel's 4x2 partial window grid
static Rect sectionRect(uint8_t slot, uint8_t sec)
{
  const RowLayout& l = layoutOf(slot);
  switch (sec)
  {
    case SEC_NAME: return { 0, int16_t(l.row1 - 1), 84, 10 };
    case SEC_IP: return { 0, int16_t(l.row2 - 1), 84, 10 };
    case SEC_DISK: return { 84, int16_t(l.row1 - 1), 68, 10 };
    case SEC_RAM: return { 84, int16_t(l.row2 - 1), 68, 10 };
    case SEC_BAR: return { 0, int16_t(l.barY - 1), SCREEN_W, int16_t(l.barH + 2) };
    default: return { 0, 0, SCREEN_W, slot ? CARD_SLOT_H : HEADER_H }; // SEC_CARD
  }
}

// ---------------------------------------------------------------- model
static const uint8_t ITEM_COUNT = 12, SLOT_COUNT = ITEMS_PER_PAGE + 1;

struct Entry
{
  char name[16];
  char ip[16];
  char disk[12];
  char ram[12];
  uint8_t bar;
  bool online;
  bool used;
};

static Entry g_header;
static Entry g_items[ITEM_COUNT];
static uint8_t g_page;

static uint8_t g_stale[SLOT_COUNT];
static bool g_staleIndicator;
static bool g_fullPending = true;
static uint32_t g_staleSince;
static uint32_t g_lastFull;
static uint8_t g_rotation = ((EPD_ROTATION % 4) + 4) % 4; // -1 -> 3

// snapshot drawn by the page loop; serial updates during busy waits only touch the live model
struct Scene
{
  Entry header;
  Entry items[ITEMS_PER_PAGE];
  uint8_t page, pages;
};
static Scene g_scene;

static const uint8_t PROTOCOL_VERSION = 1;
static char g_line[128];
static uint8_t g_lineLen;
static bool g_lineOverflow;

// the one reply per line: OK, or ERR with the reason
static void reply(const char* err)
{
  if (!err)
  {
    Serial.println("OK");
    return;
  }
  Serial.print("ERR ");
  Serial.println(err);
}

static void replyVersion()
{
  char buf[80];
  snprintf(buf, sizeof(buf), "VER %d items=%d per_page=%d n=%d ip=%d d=%d r=%d line=%d", PROTOCOL_VERSION, ITEM_COUNT,
           ITEMS_PER_PAGE, int(sizeof(g_header.name) - 1), int(sizeof(g_header.ip) - 1), int(sizeof(g_header.disk) - 1),
           int(sizeof(g_header.ram) - 1), int(sizeof(g_line) - 1));
  Serial.println(buf);
}

static void resetEntry(Entry& e)
{
  memset(&e, 0, sizeof(e));
  e.online = true;
}

static void resetHeader()
{
  resetEntry(g_header);
  strcpy(g_header.name, "No data");
}

static uint8_t pageCount()
{
  for (int8_t i = ITEM_COUNT - 1; i >= 0; i--)
  {
    if (g_items[i].used) return i / ITEMS_PER_PAGE + 1;
  }
  return 1;
}

static bool anyStale()
{
  if (g_staleIndicator) return true;
  for (uint8_t s = 0; s < SLOT_COUNT; s++)
  {
    if (g_stale[s]) return true;
  }
  return false;
}

static void markStale(uint8_t slot, uint8_t mask)
{
  if (!anyStale()) g_staleSince = millis();
  g_stale[slot] |= mask;
}

static void markIndicatorStale()
{
  if (!anyStale()) g_staleSince = millis();
  g_staleIndicator = true;
}

static void setPage(uint8_t page)
{
  if (page == g_page) return;
  g_page = page;
  for (uint8_t s = 1; s < SLOT_COUNT; s++) markStale(s, SEC_CARD);
  markIndicatorStale();
}

static void nextPage()
{
  setPage((g_page + 1) % pageCount());
}

// slot showing item idx, -1 when it is on another page
static int8_t slotOf(uint8_t idx)
{
  return (idx / ITEMS_PER_PAGE == g_page) ? idx % ITEMS_PER_PAGE + 1 : -1;
}

static bool copyIfChanged(char* dst, size_t size, const char* src)
{
  size_t n = strlen(src);
  if (n >= size) n = size - 1;
  if ((strlen(dst) == n) && (memcmp(dst, src, n) == 0)) return false;
  memcpy(dst, src, n);
  dst[n] = 0;
  return true;
}

// ---------------------------------------------------------------- serial
// idx -1 = header, fields = "k=v|k=v..." or "del"; returns the first error, nullptr if none
static const char* applyEntry(int8_t idx, char* fields)
{
  Entry& e = idx < 0 ? g_header : g_items[idx];
  int8_t slot = idx < 0 ? 0 : slotOf(idx);
  uint8_t pagesBefore = pageCount();
  if (strcmp(fields, "del") == 0)
  {
    if (idx < 0) resetHeader();
    else if (!e.used) return nullptr;
    else resetEntry(e);
    if (slot >= 0) markStale(slot, SEC_CARD);
    uint8_t pages = pageCount();
    if (pages != pagesBefore)
    {
      markIndicatorStale();
      if (g_page >= pages) setPage(pages - 1);
    }
    return nullptr;
  }
  const char* err = nullptr;
  uint8_t changed = 0;
  if ((idx >= 0) && !e.used)
  {
    e.used = true;
    changed |= SEC_CARD;
    if (pageCount() != pagesBefore) markIndicatorStale();
  }
  for (char* tok = strtok(fields, "|"); tok; tok = strtok(nullptr, "|"))
  {
    char* eq = strchr(tok, '=');
    if (!eq)
    {
      if (!err) err = "field without '='";
      continue;
    }
    *eq = 0;
    const char* val = eq + 1;
    if (strcmp(tok, "n") == 0) { if (copyIfChanged(e.name, sizeof(e.name), val)) changed |= SEC_NAME; }
    else if (strcmp(tok, "ip") == 0) { if (copyIfChanged(e.ip, sizeof(e.ip), val)) changed |= SEC_IP; }
    else if (strcmp(tok, "d") == 0) { if (copyIfChanged(e.disk, sizeof(e.disk), val)) changed |= SEC_DISK; }
    else if (strcmp(tok, "r") == 0) { if (copyIfChanged(e.ram, sizeof(e.ram), val)) changed |= SEC_RAM; }
    else if (strcmp(tok, "b") == 0)
    {
      uint8_t b = constrain(atoi(val), 0, 100);
      if (b != e.bar) { e.bar = b; changed |= SEC_BAR; }
    }
    else if (strcmp(tok, "on") == 0)
    {
      bool on = atoi(val) != 0;
      if (on != e.online) { e.online = on; changed |= SEC_CARD; } // background changes
    }
    else if (!err) err = "unknown field";
  }
  if (changed && (slot >= 0)) markStale(slot, changed);
  return err;
}

// returns the error, nullptr on success
static const char* execLine(char* line)
{
  if (strcmp(line, "clear") == 0)
  {
    for (uint8_t i = 0; i < ITEM_COUNT; i++) resetEntry(g_items[i]);
    g_page = 0;
    for (uint8_t s = 1; s < SLOT_COUNT; s++) markStale(s, SEC_CARD);
    markIndicatorStale();
  }
  else if (strcmp(line, "full") == 0) g_fullPending = true;
  else if (strcmp(line, "next") == 0) nextPage();
  else if (strncmp(line, "page=", 5) == 0)
  {
    int p = atoi(line + 5);
    if ((p < 1) || (p > pageCount())) return "no such page";
    setPage(p - 1);
  }
  else if (strncmp(line, "rot=", 4) == 0)
  {
    int r = atoi(line + 4);
    if ((r < -1) || (r > 3)) return "rotation must be -1..3";
    if (uint8_t((r + 4) % 4) != g_rotation)
    {
      // applied by the next render, never mid-frame; everything moves, so redraw it all
      g_rotation = (r + 4) % 4;
      g_fullPending = true;
    }
  }
  else
  {
    char* sep = strchr(line, '|');
    if (!sep) return "unknown command";
    *sep = 0;
    if ((strcmp(line, "H") == 0) || (strcmp(line, "h") == 0)) return applyEntry(-1, sep + 1);
    int n = atoi(line);
    if ((n < 1) || (n > ITEM_COUNT)) return "item must be H or 1..12";
    return applyEntry(n - 1, sep + 1);
  }
  return nullptr;
}

static void handleLine(char* line)
{
  if (strcmp(line, "ver") == 0) replyVersion();
  else reply(execLine(line));
}

static void pollSerial()
{
  while (Serial.available() > 0)
  {
    char c = Serial.read();
    if ((c == '\n') || (c == '\r'))
    {
      if (g_lineOverflow) reply("line too long");
      else if (g_lineLen > 0)
      {
        g_line[g_lineLen] = 0;
        handleLine(g_line);
      }
      g_lineLen = 0;
      g_lineOverflow = false;
    }
    else if (g_lineLen < sizeof(g_line) - 1) g_line[g_lineLen++] = c;
    else g_lineOverflow = true;
  }
}

static void pollButton()
{
  if (buttonPin < 0) return;
  static bool stable = HIGH, last = HIGH;
  static uint32_t changedAt;
  bool level = digitalRead(buttonPin);
  if (level != last)
  {
    last = level;
    changedAt = millis();
  }
  else if ((level != stable) && (millis() - changedAt >= BUTTON_DEBOUNCE_MS))
  {
    stable = level;
    if (stable == LOW) nextPage();
  }
}

// called by the driver while it waits for BUSY, so input isn't held up by slow refreshes
static void onBusy(const void*)
{
  pollSerial();
  pollButton();
#ifdef EPD_DEBUG
  // heartbeat: stops if the MCU locks up, keeps going (and ends in "Busy Timeout!") if BUSY is stuck
  static uint32_t lastBeat;
  if (millis() - lastBeat >= 1000)
  {
    lastBeat = millis();
    Serial.print("  busy ");
    Serial.println(lastBeat);
  }
#endif
  delay(1);
}

// ---------------------------------------------------------------- drawing
// built-in 6x8 font, with '.', ':' and ' ' narrowed so a full IPv4 address fits the text column
static int16_t charAdvance(char c)
{
  return ((c == '.') || (c == ':') || (c == ' ')) ? 3 : 6;
}

static int16_t textWidth(const char* s)
{
  int16_t w = 0;
  for (; *s; s++) w += charAdvance(*s);
  return w > 0 ? w - 1 : 0;
}

// returns the x just past the drawn text
static int16_t drawText(int16_t x, int16_t y, const char* s, int16_t maxW, uint16_t color)
{
  int16_t end = x + maxW;
  for (; *s; s++)
  {
    int16_t adv = charAdvance(*s);
    if (x + adv - 1 > end) break;
    // '.' and ':' glyphs sit in columns 1..2, shift them left to keep a 1 px gap on both sides
    if (*s != ' ') display.drawChar(((*s == '.') || (*s == ':')) ? x - 1 : x, y, *s, color, color, 1);
    x += adv;
  }
  return x;
}

static int16_t drawValue(int16_t x, int16_t y, const char* v, uint16_t color)
{
  const char* unit = VALUE_UNIT;
  if (*v && *unit)
  {
    char buf[24];
    snprintf(buf, sizeof(buf), "%s %s", v, unit);
    if (textWidth(buf) > VALUE_W) snprintf(buf, sizeof(buf), "%s%s", v, unit);
    if (textWidth(buf) > VALUE_W) snprintf(buf, sizeof(buf), "%s%c", v, unit[0]);
    if (textWidth(buf) <= VALUE_W) return drawText(x, y, buf, VALUE_W, color);
  }
  return drawText(x, y, v, VALUE_W, color);
}

// 11x9 drive
static void drawDiskIcon(int16_t x, int16_t y, uint16_t c)
{
  display.drawRoundRect(x, y, 11, 9, 2, c);
  display.drawFastHLine(x + 1, y + 4, 9, c);
  display.drawPixel(x + 2, y + 6, c);
  display.drawPixel(x + 4, y + 6, c);
}

// 12x6 memory module with three chips
static void drawRamIcon(int16_t x, int16_t y, uint16_t c)
{
  display.drawRect(x, y + 1, 12, 6, c);
  for (int16_t i = 0; i < 3; i++) display.fillRect(x + 2 + 3 * i, y + 3, 2, 2, c);
}

static uint16_t barColor(uint8_t pct, uint16_t low)
{
  return pct >= BAR_CRIT_PCT ? GxEPD_RED : pct >= BAR_WARN_PCT ? GxEPD_YELLOW : low;
}

// ends (optional) receives the x just past name, ip, disk value and ram value
static void drawRowContent(const Entry& e, int16_t y0, const RowLayout& l, uint16_t titleColor, uint16_t color, int16_t* ends = nullptr)
{
  int16_t e0 = drawText(TEXT_X, y0 + l.row1, e.name, TEXT_W, titleColor);
  int16_t e1 = drawText(TEXT_X, y0 + l.row2, e.ip, TEXT_W, color);
  drawDiskIcon(ICON_X, y0 + l.row1, color);
  int16_t e2 = drawValue(VALUE_X, y0 + l.row1, e.disk, color);
  drawRamIcon(ICON_X, y0 + l.row2, color);
  int16_t e3 = drawValue(VALUE_X, y0 + l.row2, e.ram, color);
  if (ends)
  {
    ends[0] = e0;
    ends[1] = e1;
    ends[2] = e2;
    ends[3] = e3;
  }
}

// true if (x, dy) lies on a card text row next to drawn content, with a 1 px margin
static bool onContent(int16_t x, int16_t dy, const int16_t* ends)
{
  for (uint8_t row = 0; row < 2; row++)
  {
    int16_t top = row ? CARD_LAYOUT.row2 : CARD_LAYOUT.row1;
    if ((dy < top - 1) || (dy > top + 8)) continue;
    if ((x >= TEXT_X - 1) && (x <= ends[row])) return true;
    if ((x >= ICON_X - 1) && (x <= ends[row + 2])) return true;
  }
  return false;
}

static void drawHeader(const Entry& e)
{
  drawRowContent(e, 0, HEADER_LAYOUT, GxEPD_YELLOW, GxEPD_WHITE);
  int16_t w = int32_t(SCREEN_W) * e.bar / 100;
  if (w > 0) display.fillRect(0, HEADER_LAYOUT.barY, w, HEADER_LAYOUT.barH, barColor(e.bar, GxEPD_WHITE));
}

static void drawCard(uint8_t i, const Entry& e)
{
  if (!e.used) return;
  int16_t y0 = slotY(i + 1);
  display.fillRoundRect(CARD_X, y0, CARD_W, CARD_H, CARD_R, GxEPD_WHITE);
  int16_t ends[4];
  drawRowContent(e, y0, CARD_LAYOUT, GxEPD_BLACK, GxEPD_BLACK, ends);
  if (!e.online)
  {
    // offline: evenly spaced solid diagonals ('/', 2 px thick, 6 px apart), broken around text and icons
    for (int16_t dy = 2; dy < CARD_H - 2; dy++)
    {
      for (int16_t x = CARD_X + 2; x < CARD_X + CARD_W - 2; x++)
      {
        if (((x + dy) % 6 < 2) && !onContent(x, dy, ends)) display.drawPixel(x, y0 + dy, GxEPD_BLACK);
      }
    }
  }
  int16_t w = int32_t(CARD_W - 4) * e.bar / 100;
  if (w > 0) display.fillRect(CARD_X + 2, y0 + CARD_LAYOUT.barY, w, CARD_LAYOUT.barH, barColor(e.bar, GxEPD_BLACK));
}

static void drawPageIndicator(uint8_t page, uint8_t pages)
{
  if (pages < 2) return;
  const int16_t gap = 4;
  int16_t dash = (SCREEN_W - 8 - (pages - 1) * gap) / pages; // shrinks so 12 pages (1 per page) still fit
  if (dash > 10) dash = 10;
  int16_t x = (SCREEN_W - (pages * dash + (pages - 1) * gap)) / 2;
  for (uint8_t p = 0; p < pages; p++, x += dash + gap)
  {
    if (p == page) display.fillRect(x, INDICATOR_Y, dash, INDICATOR_H, GxEPD_WHITE);
    else display.fillRect(x + (dash - 2) / 2, INDICATOR_Y, 2, INDICATOR_H, GxEPD_WHITE);
  }
}

// draws everything; GxEPD2 clips to the current page and partial window
static void drawScene()
{
  display.fillScreen(GxEPD_BLACK);
  drawHeader(g_scene.header);
  for (uint8_t i = 0; i < ITEMS_PER_PAGE; i++) drawCard(i, g_scene.items[i]);
  drawPageIndicator(g_scene.page, g_scene.pages);
}

static void unionRect(int16_t& x0, int16_t& y0, int16_t& x1, int16_t& y1, const Rect& r, int16_t dy)
{
  x0 = min(x0, r.x);
  y0 = min(y0, int16_t(r.y + dy));
  x1 = max(x1, int16_t(r.x + r.w));
  y1 = max(y1, int16_t(r.y + dy + r.h));
}

static void render(bool full)
{
  display.setRotation(g_rotation);
  g_scene.header = g_header;
  for (uint8_t i = 0; i < ITEMS_PER_PAGE; i++) g_scene.items[i] = g_items[g_page * ITEMS_PER_PAGE + i];
  g_scene.page = g_page;
  g_scene.pages = pageCount();

  // bounding box of everything stale, one partial refresh per pass
  int16_t x0 = SCREEN_W, y0 = SCREEN_W, x1 = 0, y1 = 0;
  for (uint8_t s = 0; s < SLOT_COUNT; s++)
  {
    for (uint8_t b = 0; b < SEC_COUNT; b++)
    {
      if (g_stale[s] & (1 << b)) unionRect(x0, y0, x1, y1, sectionRect(s, 1 << b), slotY(s));
    }
    g_stale[s] = 0;
  }
  if (g_staleIndicator) unionRect(x0, y0, x1, y1, INDICATOR_RECT, 0);
  g_staleIndicator = false;
  if (full) g_fullPending = false;
  else if (x1 <= x0) return;

  if (ledPin >= 0) digitalWrite(ledPin, LED_ON_LEVEL);
#ifdef EPD_DEBUG
  Serial.print(full ? "render full" : "render partial ");
  if (!full) { Serial.print(x0); Serial.print(','); Serial.print(y0); Serial.print(' '); Serial.print(x1 - x0); Serial.print('x'); Serial.print(y1 - y0); }
  Serial.println();
  uint32_t started = millis();
#endif
  if (full) display.setFullWindow();
  else
  {
    x0 &= ~3;
    y0 &= ~1;
    x1 = min(int16_t((x1 + 3) & ~3), SCREEN_W);
    y1 = min(int16_t((y1 + 1) & ~1), SCREEN_W);
    display.setPartialWindow(x0, y0, x1 - x0, y1 - y0);
  }
  display.firstPage();
#ifdef EPD_DEBUG
  uint8_t pageNo = 0;
#endif
  do
  {
#ifdef EPD_DEBUG
    Serial.print("  page ");
    Serial.println(pageNo++);
#endif
    drawScene();
  }
  while (display.nextPage());
  display.hibernate(); // vendor: sleep after every update; the next one starts with a clean reset
  if (ledPin >= 0) digitalWrite(ledPin, !LED_ON_LEVEL);
#ifdef EPD_DEBUG
  Serial.print("render done in ");
  Serial.print(millis() - started);
  Serial.println(" ms");
  if (millis() - started < 250) Serial.println("  ^ too fast for e-paper: no waveform ran (check BUSY/RST wiring)");
#endif
  if (full) g_lastFull = millis();
}

// ---------------------------------------------------------------- setup / loop
// frame rate in Hz to the driver's PLL code, nearest supported rate
static int8_t frameRateCode(uint16_t hz)
{
  if (hz == 0) return -1;
  static const uint8_t rates[] = { 12, 25, 50, 65, 75, 85, 100, 120 }; // code 0 is 12.5 Hz
  int8_t best = 0;
  for (int8_t i = 1; i < 8; i++)
  {
    if (abs(int(rates[i]) - int(hz)) < abs(int(rates[best]) - int(hz))) best = i;
  }
  return best;
}

void setup()
{
#if defined(ESP32)
  Serial.setRxBufferSize(1024);
#endif
  Serial.begin(SERIAL_BAUD);
  if (buttonPin >= 0) pinMode(buttonPin, INPUT_PULLUP);
  if (ledPin >= 0)
  {
    pinMode(ledPin, OUTPUT);
    digitalWrite(ledPin, !LED_ON_LEVEL);
  }
  resetHeader();
  for (uint8_t i = 0; i < ITEM_COUNT; i++) resetEntry(g_items[i]);

  // GxEPD2::init() presets these with digitalWrite() before pinMode(), which core 3.x logs as an error
  pinMode(EPD_CS_PIN, OUTPUT);
  digitalWrite(EPD_CS_PIN, HIGH);
  pinMode(EPD_DC_PIN, OUTPUT);
  digitalWrite(EPD_DC_PIN, HIGH);
  pinMode(EPD_RST_PIN, OUTPUT);
  digitalWrite(EPD_RST_PIN, HIGH);
#if defined(ARDUINO_ARCH_STM32)
  SPI.setSCLK(EPD_SCK_PIN);
  SPI.setMOSI(EPD_MOSI_PIN);
  SPI.begin(); // CS is driven by GxEPD2
#else
  SPI.begin(EPD_SCK_PIN, -1, EPD_MOSI_PIN, -1); // CS is driven by GxEPD2
#endif
  display.epd2.selectSPI(SPI, SPISettings(EPD_SPI_HZ, MSBFIRST, SPI_MODE0));
#ifdef EPD_DEBUG
  // USB CDC drops output until the host opens the port; give it a moment
  for (uint32_t t = millis(); !Serial && (millis() - t < 3000);) {}
  Serial.println("StatMonitorEPD boot");
#endif
  // no driver diagnostics: they call Serial.begin() again on the USB CDC; onBusy's heartbeat covers timing
  display.init(0, true, 40, false); // 40 ms reset pulse
  display.epd2.setBusyCallback(onBusy);
  display.epd2.setFrameRate(frameRateCode(EPD_FRAME_RATE));
#ifdef EPD_TEMPERATURE
  display.epd2.setFastTemperature(uint8_t(int8_t(EPD_TEMPERATURE)));
#endif
  display.epd2.setAbortMs(EPD_ABORT_MS);
  // rotation is set by render() from g_rotation
}

void loop()
{
  pollSerial();
  pollButton();
  uint32_t now = millis();
  if (g_fullPending || (now - g_lastFull >= FULL_REFRESH_MS)) render(true);
  else if (anyStale() && (now - g_staleSince >= UPDATE_HOLDOFF_MS)) render(false);
}
