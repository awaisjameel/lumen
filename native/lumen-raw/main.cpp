// lumen-raw is Lumen's camera RAW worker: one process per file, so a
// malformed or enormous RAW has a lifetime and a memory budget of its own
// (the engine puts it in a job object and kills it when a request goes away).
// It links LibRaw statically, reads the one file it is given, and writes its
// answer to standard output. It never writes a file.
//
//   lumen-raw info    <file>          what the file says about itself
//   lumen-raw develop <file> <side>   the sensor data developed to upright sRGB
//   lumen-raw preview <file>          the largest JPEG the camera embedded
//
// Every answer starts with one line of JSON. develop follows it with
// width x height x 3 bytes of 8-bit RGB; preview follows it with the JPEG.
// develop halves the picture while that still leaves <side> pixels on its
// long edge (0 = never), which is several times faster and is how thumbnails
// are made.
//
// The development policy is fixed, and part of media.RawVersion on the Go
// side: the camera's white balance (daylight where it recorded none), the
// camera's colour matrix, sRGB primaries and tone curve, 8 bits, LibRaw's
// automatic brightening (1% clipped), highlights clipped, the camera's
// orientation applied. Change any of it and bump that version.
//
// Exit codes: 0 done; 2 misuse; 3 LibRaw cannot read this file or this part
// of it (the caller may fall back to the embedded preview); 4 anything else.

#include <cmath>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <ctime>
#include <string>

#include <fcntl.h>
#include <io.h>

#include "libraw/libraw.h"

namespace {

const int exitMisuse = 2;
const int exitUnsupported = 3;
const int exitFailed = 4;

// The most sensor pixels a file may declare, and the memory LibRaw may take
// for them. The engine's job object is the real limit; these refuse a file
// that could never fit before anything is allocated.
const double maxPixels = 250e6;
const unsigned maxRawMemoryMB = 1024;

int failure(int code) {
  switch (code) {
  case LIBRAW_FILE_UNSUPPORTED:
  case LIBRAW_NOT_IMPLEMENTED:
  case LIBRAW_NO_THUMBNAIL:
  case LIBRAW_UNSUPPORTED_THUMBNAIL:
  case LIBRAW_REQUEST_FOR_NONEXISTENT_IMAGE:
  case LIBRAW_REQUEST_FOR_NONEXISTENT_THUMBNAIL:
  case LIBRAW_DATA_ERROR:
  case LIBRAW_TOO_BIG:
    return exitUnsupported;
  }
  return exitFailed;
}

int fail(const char *what, int code) {
  fprintf(stderr, "lumen-raw: %s: %s\n", what, libraw_strerror(code));
  return failure(code);
}

// failRead is fail for reading one part of a file that did open. Running out
// of file there is the file's fault (it was cut short), not the disk's, and
// another part of it may still read.
int failRead(const char *what, int code) {
  int exit = fail(what, code);
  return code == LIBRAW_IO_ERROR ? exitUnsupported : exit;
}

// quoted writes s as a JSON string. Camera text is ASCII in practice; any
// other byte is dropped rather than guessed at.
std::string quoted(const char *s) {
  std::string out = "\"";
  for (; *s; s++) {
    unsigned char c = static_cast<unsigned char>(*s);
    if (c == '"' || c == '\\') {
      out += '\\';
      out += static_cast<char>(c);
    } else if (c >= 0x20 && c < 0x7f) {
      out += static_cast<char>(c);
    }
  }
  // Fixed-width camera fields are often padded with spaces.
  while (out.size() > 1 && out.back() == ' ')
    out.pop_back();
  return out + "\"";
}

std::string number(double v) {
  if (!std::isfinite(v))
    return "0";
  char buf[64];
  snprintf(buf, sizeof buf, "%.9g", v);
  return buf;
}

// The EXIF orientation that LibRaw's flip stands for.
int orientation(int flip) {
  switch (flip) {
  case 3:
    return 3;
  case 5:
    return 8;
  case 6:
    return 6;
  }
  return 1;
}

// largestJPEG is the index of the biggest embedded JPEG, or -1.
int largestJPEG(const libraw_thumbnail_list_t &list) {
  int best = -1;
  long bestArea = 0;
  for (int i = 0; i < list.thumbcount && i < LIBRAW_THUMBNAIL_MAXCOUNT; i++) {
    const libraw_thumbnail_item_t &t = list.thumblist[i];
    long area = static_cast<long>(t.twidth) * t.theight;
    if (t.tformat == LIBRAW_INTERNAL_THUMBNAIL_JPEG && area > bestArea) {
      best = i;
      bestArea = area;
    }
  }
  return best;
}

bool writeAll(const void *data, size_t size) {
  return fwrite(data, 1, size, stdout) == size && fflush(stdout) == 0;
}

int info(LibRaw &raw) {
  if (int err = raw.adjust_sizes_info_only(); err != LIBRAW_SUCCESS)
    return fail("read sizes", err);
  const libraw_data_t &d = raw.imgdata;
  std::string out = "{";
  out += "\"make\":" + quoted(d.idata.make);
  out += ",\"model\":" + quoted(d.idata.model);
  // After adjust_sizes_info_only these are the developed picture's, upright.
  out += ",\"width\":" + std::to_string(d.sizes.iwidth);
  out += ",\"height\":" + std::to_string(d.sizes.iheight);
  out += ",\"dng\":" + std::string(d.idata.dng_version ? "true" : "false");
  if (d.other.timestamp > 0) {
    // LibRaw turned the camera's wall-clock time into a time_t with this
    // process's time zone, so the same zone gives the wall clock back.
    time_t t = d.other.timestamp;
    struct tm local;
    if (localtime_s(&local, &t) == 0) {
      char buf[32];
      strftime(buf, sizeof buf, "%Y-%m-%dT%H:%M:%S", &local);
      out += ",\"taken\":" + quoted(buf);
    }
  }
  out += ",\"iso\":" + number(d.other.iso_speed);
  out += ",\"shutter\":" + number(d.other.shutter);
  out += ",\"aperture\":" + number(d.other.aperture);
  out += ",\"focal\":" + number(d.other.focal_len);
  out += ",\"lens\":" + quoted(d.lens.Lens);
  const libraw_gps_info_t &g = d.other.parsed_gps;
  if (g.gpsparsed && (g.latref == 'N' || g.latref == 'S') && (g.longref == 'E' || g.longref == 'W')) {
    double lat = g.latitude[0] + g.latitude[1] / 60.0 + g.latitude[2] / 3600.0;
    double lon = g.longitude[0] + g.longitude[1] / 60.0 + g.longitude[2] / 3600.0;
    out += ",\"gps\":true,\"lat\":" + number(g.latref == 'S' ? -lat : lat);
    out += ",\"lon\":" + number(g.longref == 'W' ? -lon : lon);
  }
  int thumb = largestJPEG(d.thumbs_list);
  if (thumb >= 0) {
    out += ",\"previewWidth\":" + std::to_string(d.thumbs_list.thumblist[thumb].twidth);
    out += ",\"previewHeight\":" + std::to_string(d.thumbs_list.thumblist[thumb].theight);
  }
  out += "}\n";
  return writeAll(out.data(), out.size()) ? 0 : exitFailed;
}

int develop(LibRaw &raw, int side) {
  libraw_data_t &d = raw.imgdata;
  if (static_cast<double>(d.sizes.raw_width) * d.sizes.raw_height > maxPixels)
    return fail("sensor size", LIBRAW_TOO_BIG);
  libraw_output_params_t &p = d.params;
  p.use_camera_wb = 1;
  p.use_camera_matrix = 1;
  p.output_color = 1; // sRGB primaries
  p.gamm[0] = 1 / 2.4; // and sRGB's tone curve, which dcraw's default is not
  p.gamm[1] = 12.92;
  p.output_bps = 8;
  p.highlight = 0;
  p.user_flip = -1; // the camera's orientation
  // Fujifilm's X-Trans pattern: one interpolation pass, not LibRaw's three,
  // which take ten times as long for a difference a gallery does not show.
  if (d.idata.filters == LIBRAW_XTRANS)
    p.user_qual = 2;
  int longEdge = d.sizes.width > d.sizes.height ? d.sizes.width : d.sizes.height;
  p.half_size = side > 0 && longEdge / 2 >= side;
  if (int err = raw.unpack(); err != LIBRAW_SUCCESS)
    return failRead("unpack", err);
  if (int err = raw.dcraw_process(); err != LIBRAW_SUCCESS)
    return failRead("develop", err);
  int err = LIBRAW_SUCCESS;
  libraw_processed_image_t *im = raw.dcraw_make_mem_image(&err);
  if (!im)
    return fail("make image", err);
  int code = exitFailed;
  size_t pixels = static_cast<size_t>(im->width) * im->height;
  if (im->type == LIBRAW_IMAGE_BITMAP && im->bits == 8 && (im->colors == 3 || im->colors == 1) &&
      im->data_size >= pixels * im->colors) {
    std::string head = "{\"width\":" + std::to_string(im->width) + ",\"height\":" + std::to_string(im->height) +
                       ",\"half\":" + (p.half_size ? "true" : "false") + "}\n";
    bool ok = writeAll(head.data(), head.size());
    if (ok && im->colors == 3) {
      ok = writeAll(im->data, pixels * 3);
    } else if (ok) { // a monochrome sensor: grey as RGB
      std::string row(static_cast<size_t>(im->width) * 3, '\0');
      for (int y = 0; ok && y < im->height; y++) {
        const unsigned char *src = im->data + static_cast<size_t>(y) * im->width;
        for (int x = 0; x < im->width; x++)
          row[3 * x] = row[3 * x + 1] = row[3 * x + 2] = static_cast<char>(src[x]);
        ok = writeAll(row.data(), row.size());
      }
    }
    code = ok ? 0 : exitFailed;
  } else {
    fprintf(stderr, "lumen-raw: unexpected image layout\n");
    code = exitUnsupported;
  }
  LibRaw::dcraw_clear_mem(im);
  return code;
}

int preview(LibRaw &raw) {
  libraw_data_t &d = raw.imgdata;
  int index = largestJPEG(d.thumbs_list);
  if (index < 0)
    return fail("find preview", LIBRAW_NO_THUMBNAIL);
  if (int err = raw.unpack_thumb_ex(index); err != LIBRAW_SUCCESS)
    return failRead("unpack preview", err);
  const libraw_thumbnail_t &t = d.thumbnail;
  if (t.tformat != LIBRAW_THUMBNAIL_JPEG || !t.thumb || t.tlength < 4)
    return fail("read preview", LIBRAW_UNSUPPORTED_THUMBNAIL);
  // The preview is stored as the sensor saw it, like the RAW data; a
  // preview with a flip of its own says so, otherwise the picture's applies.
  int flip = d.thumbs_list.thumblist[index].tflip;
  if (flip == 0xffff)
    flip = d.sizes.flip;
  std::string head = "{\"width\":" + std::to_string(t.twidth) + ",\"height\":" + std::to_string(t.theight) +
                     ",\"orientation\":" + std::to_string(orientation(flip)) + "}\n";
  return writeAll(head.data(), head.size()) && writeAll(t.thumb, t.tlength) ? 0 : exitFailed;
}

} // namespace

int wmain(int argc, wchar_t **argv) {
  if (argc < 3)
    return exitMisuse;
  std::wstring op = argv[1];
  if (_setmode(_fileno(stdout), _O_BINARY) == -1)
    return exitFailed;
  LibRaw raw;
  raw.imgdata.rawparams.max_raw_memory_mb = maxRawMemoryMB;
  if (int err = raw.open_file(argv[2]); err != LIBRAW_SUCCESS)
    return fail("open", err);
  if (op == L"info" && argc == 3)
    return info(raw);
  if (op == L"preview" && argc == 3)
    return preview(raw);
  if (op == L"develop" && argc == 4)
    return develop(raw, _wtoi(argv[3]));
  return exitMisuse;
}
