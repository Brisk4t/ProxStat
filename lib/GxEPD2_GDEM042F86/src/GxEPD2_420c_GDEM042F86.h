// GxEPD2 driver for the Good Display GDEM042F86 (4.2", 400x300, black/white/yellow/red).
// Panel: GDEM042F86 : https://www.good-display.com/product/1048.html
// Controller: SSD2683 (Solomon Systech)
//
// Derived from GxEPD2_420c_GDEY0420F51 of GxEPD2 by Jean-Marc Zingg (GPL-3.0),
// https://github.com/ZinggJM/GxEPD2 . The data path (2bpp, 00=black 01=white
// 10=yellow 11=red) and the PTLW (0x83) partial window command are identical on
// the SSD2683; the init sequence is taken from the Good Display GDEM042F86 demo.
//
// Partial window refresh: the SSD2683 PTLW command only redraws the window, but
// the 4-color waveform still runs (with flashing) inside it. Define
// GDEM042F86_NO_PARTIAL to fall back to full screen refresh only.

#ifndef _GxEPD2_420c_GDEM042F86_H_
#define _GxEPD2_420c_GDEM042F86_H_

#include <GxEPD2_EPD.h>

class GxEPD2_420c_GDEM042F86 : public GxEPD2_EPD
{
  public:
    // attributes
    static const uint16_t WIDTH = 400;
    static const uint16_t WIDTH_VISIBLE = WIDTH;
    static const uint16_t HEIGHT = 300;
    static const GxEPD2::Panel panel = GxEPD2::GDEY0420F51;
    static const bool hasColor = true;
#if defined(GDEM042F86_NO_PARTIAL)
    static const bool hasPartialUpdate = false;
#else
    static const bool hasPartialUpdate = true; // uses PTLW (0x83) partial window
#endif
    static const bool hasFastPartialUpdate = false;
    static const uint16_t power_on_time = 100; // ms, e.g. 65197us
    static const uint16_t power_off_time = 100; // ms, e.g. 84464us
    static const uint16_t full_refresh_time = 25000; // ms, ~20s normal, ~12s fast
    static const uint16_t partial_refresh_time = 25000; // ms, same waveform as full
    // constructor
    GxEPD2_420c_GDEM042F86(int16_t cs, int16_t dc, int16_t rst, int16_t busy);
    // methods (virtual)
    //  Support for Bitmaps (Sprites) to Controller Buffer and to Screen
    void clearScreen(uint8_t value = 0xFF); // init controller memory and screen (default white)
    void clearScreen(uint8_t black_value, uint8_t color_value); // init controller memory and screen
    void writeScreenBuffer(uint8_t value = 0xFF); // init controller memory (default white)
    void writeScreenBuffer(uint8_t black_value, uint8_t color_value); // init controller memory
    // write to controller memory, without screen refresh; x and w should be multiple of 8
    void writeImage(const uint8_t bitmap[], int16_t x, int16_t y, int16_t w, int16_t h, bool invert = false, bool mirror_y = false, bool pgm = false);
    void writeImagePart(const uint8_t bitmap[], int16_t x_part, int16_t y_part, int16_t w_bitmap, int16_t h_bitmap,
                        int16_t x, int16_t y, int16_t w, int16_t h, bool invert = false, bool mirror_y = false, bool pgm = false);
    void writeImage(const uint8_t* black, const uint8_t* color, int16_t x, int16_t y, int16_t w, int16_t h, bool invert = false, bool mirror_y = false, bool pgm = false);
    void writeImagePart(const uint8_t* black, const uint8_t* color, int16_t x_part, int16_t y_part, int16_t w_bitmap, int16_t h_bitmap,
                        int16_t x, int16_t y, int16_t w, int16_t h, bool invert = false, bool mirror_y = false, bool pgm = false);
    // write sprite of native data to controller memory, without screen refresh; x and w should be multiple of 8
    void writeNative(const uint8_t* data1, const uint8_t* data2, int16_t x, int16_t y, int16_t w, int16_t h, bool invert = false, bool mirror_y = false, bool pgm = false);
    void writeNativePart(const uint8_t* data1, const uint8_t* data2, int16_t x_part, int16_t y_part, int16_t w_bitmap, int16_t h_bitmap,
                        int16_t x, int16_t y, int16_t w, int16_t h, bool invert = false, bool mirror_y = false, bool pgm = false);
    // write to controller memory, with screen refresh; x and w should be multiple of 8
    void drawImage(const uint8_t bitmap[], int16_t x, int16_t y, int16_t w, int16_t h, bool invert = false, bool mirror_y = false, bool pgm = false);
    void drawImagePart(const uint8_t bitmap[], int16_t x_part, int16_t y_part, int16_t w_bitmap, int16_t h_bitmap,
                       int16_t x, int16_t y, int16_t w, int16_t h, bool invert = false, bool mirror_y = false, bool pgm = false);
    void drawImage(const uint8_t* black, const uint8_t* color, int16_t x, int16_t y, int16_t w, int16_t h, bool invert = false, bool mirror_y = false, bool pgm = false);
    void drawImagePart(const uint8_t* black, const uint8_t* color, int16_t x_part, int16_t y_part, int16_t w_bitmap, int16_t h_bitmap,
                       int16_t x, int16_t y, int16_t w, int16_t h, bool invert = false, bool mirror_y = false, bool pgm = false);
    // write sprite of native data to controller memory, with screen refresh; x and w should be multiple of 8
    void drawNative(const uint8_t* data1, const uint8_t* data2, int16_t x, int16_t y, int16_t w, int16_t h, bool invert = false, bool mirror_y = false, bool pgm = false);
    void refresh(bool partial_update_mode = false); // screen refresh from controller memory to full screen
    void refresh(int16_t x, int16_t y, int16_t w, int16_t h); // screen refresh from controller memory, partial screen
    void powerOff(); // turns off generation of panel driving voltages, avoids screen fading over time
    void hibernate(); // turns powerOff() and sets controller to deep sleep for minimum power use, ONLY if wakeable by RST (rst >= 0)
    void setPaged(); // for GxEPD2_154c paged workaround, also for 4-color for use with hasPartialUpdate = false
    void selectFastFullUpdate(bool fast); // true: vendor "fast" (~12s) waveform, false: normal (~20s)
    // speed tuning (outside the vendor's tested settings; effects are panel dependent)
    // frame rate code for PLL 0x30 (SSD2683 datasheet, same encoding as JD79660):
    // 0=12.5 1=25 2=50 3=65 4=75 5=85 6=100 7=120 Hz, -1 = vendor default (0x08, dynamic frame rate)
    void setFrameRate(int8_t fr) { _pll = (fr < 0) ? 0x08 : (fr & 0x07); _init_display_done = false; }
    // forced temperature TSSET (0xE6) for the fast waveform, 1 degC/step signed; selects the OTP waveform band.
    // Vendor fast value is 0x5A (90). Only used while selectFastFullUpdate(true).
    void setFastTemperature(uint8_t t) { _fast_temp = t; _init_display_done = false; }
    // stop a partial refresh after this many ms by resetting the controller, 0 = let it finish.
    // Leaves the panel charge unbalanced (ghosting, colors not fully driven); use at your own risk.
    void setAbortMs(uint16_t ms) { _abort_ms = ms; }
    uint8_t  pll() const { return _pll; }
    uint8_t  fastTemperature() const { return _fast_temp; }
    uint16_t abortMs() const { return _abort_ms; }
  private:
    void _refresh(bool partial_update_mode);
    void _setPartialRamArea(uint16_t x, uint16_t y, uint16_t w, uint16_t h, bool partial_mode = true);
    void _PowerOn();
    void _PowerOff();
    void _InitDisplay();
  private:
    bool _paged;
    bool _fast;
    uint8_t _pll;
    uint8_t _fast_temp;
    uint16_t _abort_ms;
};

#endif
