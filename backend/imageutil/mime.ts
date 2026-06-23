// PORT: backend/imageutil/mime.go

const png_signature = new Uint8Array([
  0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
]);

const bytes_equal = (a: Uint8Array, b: Uint8Array): boolean => {
  if (a.length !== b.length) {
    return false;
  }
  for (let i = 0; i < a.length; i++) {
    if (a[i] !== b[i]) {
      return false;
    }
  }
  return true;
};

export const detect_supported_image_mime_type = (data: Uint8Array): string => {
  if (data.length >= 3 && bytes_equal(data.slice(0, 3), new Uint8Array([0xff, 0xd8, 0xff]))) {
    if (data.length > 3 && data[3] === 0xf7) {
      return "";
    }
    return "image/jpeg";
  }

  if (
    data.length >= png_signature.length &&
    bytes_equal(data.slice(0, png_signature.length), png_signature)
  ) {
    if (is_png(data) && !is_animated_png(data)) {
      return "image/png";
    }
    return "";
  }

  const decoder = new TextDecoder("latin1");
  if (data.length >= 3 && decoder.decode(data.slice(0, 3)) === "GIF") {
    return "image/gif";
  }

  if (
    data.length >= 12 &&
    decoder.decode(data.slice(0, 4)) === "RIFF" &&
    decoder.decode(data.slice(8, 12)) === "WEBP"
  ) {
    return "image/webp";
  }

  return "";
};

export const is_png = (data: Uint8Array): boolean => {
  if (data.length < 16) {
    return false;
  }
  const view = new DataView(data.buffer, data.byteOffset);
  const length = view.getUint32(png_signature.length, false);
  const decoder = new TextDecoder("latin1");
  return length === 13 && decoder.decode(data.slice(12, 16)) === "IHDR";
};

export const is_animated_png = (data: Uint8Array): boolean => {
  let offset = png_signature.length;
  const view = new DataView(data.buffer, data.byteOffset);
  const decoder = new TextDecoder("latin1");

  while (offset + 8 <= data.length) {
    const chunk_length = view.getUint32(offset, false);
    const chunk_type = decoder.decode(data.slice(offset + 4, offset + 8));
    if (chunk_type === "acTL") {
      return true;
    }
    if (chunk_type === "IDAT") {
      return false;
    }
    const next_offset = offset + 8 + chunk_length + 4;
    if (next_offset <= offset || next_offset > data.length) {
      return false;
    }
    offset = next_offset;
  }
  return false;
};

/*
PORT STATUS
source path: backend/imageutil/mime.go
source lines: 58
draft lines: 98
confidence: high
status: phase_a_draft
todos:
  - decide if latin1 TextDecoder is acceptable for magic-string comparisons
  - verify DataView slicing does not break on subarray/offset edge cases
notes:
  - No (T, error) returns; plain function port.
*/
