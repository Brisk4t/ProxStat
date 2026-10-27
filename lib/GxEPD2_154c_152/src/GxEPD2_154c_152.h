// Local copy of GxEPD2_154c_GDEM0154F51H resized to 152x152 for a 1.54" 4-color panel
// (black/white/yellow/red) with the same JD79660-family controller. Only WIDTH/HEIGHT
// differ; the resolution (TRES 0x61) and partial window are derived from them.
//
// Display Library for SPI e-paper panels from Dalian Good Display and boards from Waveshare.
// Requires HW SPI and Adafruit_GFX. Caution: the e-paper panels require 3.3V supply AND data lines!
//
// based on Demo Example from Good Display: https://www.good-display.com/companyfile/1521.html
// Panel: GDEM0154F51H : https://www.good-display.com/product/555.html
// Controller: JD79660 : https://v4.cecdn.yun300.cn/100001_1909185148/JD79660A_datasheet_v1.0.3_20230620.pdf
//
// Author: Jean-Marc Zingg
//
// Version: see library.properties
//
// Library: https://github.com/ZinggJM/GxEPD2

#ifndef _GxEPD2_154c_152_H_
#define _GxEPD2_154c_152_H_

#include <GxEPD2_EPD.h>

class GxEPD2_154c_152 : public GxEPD2_EPD
{
  public:
    // attributes
    static const uint16_t WIDTH = 152;
    static const uint16_t WIDTH_VISIBLE = WIDTH;
    static const uint16_t HEIGHT = 152;
    static const GxEPD2::Panel panel = GxEPD2::GDEM0154F51H;
    static const bool hasColor = true;
    static const bool hasPartialUpdate = true; // can be switched off
    static const bool hasFastPartialUpdate = false;
    static const bool useFastFullUpdate = true; // set false for extended (low) temperature range
    static const uint16_t power_on_time = 200; // ms, e.g. 157485us
    static const uint16_t power_off_time = 100; // ms, e.g. 81338us
    static const uint16_t full_refresh_time = 25000; // ms, e.g. 21428295us
    static const uint16_t partial_refresh_time = 25000; // ms, e.g. 21428295us
    // constructor
    GxEPD2_154c_152(int16_t cs, int16_t dc, int16_t rst, int16_t busy);
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
    // speed tuning (outside the vendor's tested settings; effects are panel dependent)
    // frame rate code for PLL 0x30: 0=12.5 1=25 2=50 3=65 4=75 5=85 6=100 7=120 Hz, -1 = vendor default (dynamic)
    void setFrameRate(int8_t fr) { _pll = (fr < 0) ? 0x08 : (fr & 0x07); _init_display_done = false; }
    // forced temperature for the fast waveform (0xE6); selects which OTP waveform band is used
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
    uint8_t _invert(uint8_t data); // special inversion for demo bitmap
    void selectFastFullUpdate(bool);
  private:
    bool _paged;
    bool _use_fast_update;
    uint8_t _pll;
    uint8_t _fast_temp;
    uint16_t _abort_ms;
};

#endif
