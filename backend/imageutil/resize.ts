// PORT: backend/imageutil/resize.go

import { Effect } from "effect";

export const max_bytes = Math.floor(4.5 * 1024 * 1024);

export type image_error = {
  readonly _tag: "ImageError";
  readonly reason: string;
  readonly cause: unknown;
};

export const image_error = (reason: string, cause: unknown): image_error => ({
  _tag: "ImageError",
  reason,
  cause,
});

export type resize_image_result = {
  resized_data: Uint8Array;
  width: number;
  height: number;
  original_width: number;
  original_height: number;
  was_resized: boolean;
};

// TODO: register webp decoder once a JS webp library is chosen.
export const register_webp_format = (): void => {
  // image.RegisterFormat("webp", "RIFF????WEBP", webp.Decode, webp.DecodeConfig)
};

const decode_image = (_data: Uint8Array): Effect.Effect<ImageData, image_error> =>
  Effect.fail(image_error("image decoding not implemented in Phase A draft", null));

// Encoding stubs — real implementation will use sharp, canvas, or WASM codecs.
const encode_png = (_img: ImageData): string | null => null;
const encode_jpeg = (_img: ImageData, _quality: number): string | null => null;

// ResizeImage resizes the image data to fit within 2000x2000 and 4.5MB base64 size limit.
// If no resizing is needed, it returns the original data.
// Returns an error if the image cannot be resized below the limit.
export const resize_image = (
  original_data: Uint8Array,
  _mime_type: string,
): Effect.Effect<resize_image_result, image_error> =>
  Effect.gen(function* () {
    const decoded_img = yield* decode_image(original_data);

    const original_width = decoded_img.width;
    const original_height = decoded_img.height;

    const orig_base64_len = Math.floor((original_data.length + 2) / 3) * 4;
    if (original_width <= 2000 && original_height <= 2000 && orig_base64_len < max_bytes) {
      return {
        resized_data: original_data,
        width: original_width,
        height: original_height,
        original_width,
        original_height,
        was_resized: false,
      };
    }

    let target_width = original_width;
    let target_height = original_height;

    if (target_width > 2000) {
      target_height = Math.round(target_height * (2000.0 / target_width));
      target_width = 2000;
    }
    if (target_height > 2000) {
      target_width = Math.round(target_width * (2000.0 / target_height));
      target_height = 2000;
    }
    if (target_width < 1) target_width = 1;
    if (target_height < 1) target_height = 1;

    let current_width = target_width;
    let current_height = target_height;

    while (true) {
      // Real scaling would happen here; stub uses the decoded image as-is.
      const dst = decoded_img;

      const png_base64 = encode_png(dst);
      const jpeg_base64 = encode_jpeg(dst, 80);

      let best_base64: string | null = null;

      if (
        png_base64 !== null &&
        png_base64.length < max_bytes &&
        jpeg_base64 !== null &&
        jpeg_base64.length < max_bytes
      ) {
        best_base64 = png_base64.length < jpeg_base64.length ? png_base64 : jpeg_base64;
      } else if (png_base64 !== null && png_base64.length < max_bytes) {
        best_base64 = png_base64;
      } else if (jpeg_base64 !== null && jpeg_base64.length < max_bytes) {
        best_base64 = jpeg_base64;
      }

      if (best_base64 !== null) {
        const decoded_best = Buffer.from(best_base64, "base64");
        return {
          resized_data: new Uint8Array(decoded_best),
          width: current_width,
          height: current_height,
          original_width,
          original_height,
          was_resized: true,
        };
      }

      let found_jpeg = false;
      for (const q of [70, 55, 40]) {
        const q_base64 = encode_jpeg(dst, q);
        if (q_base64 !== null && q_base64.length < max_bytes) {
          best_base64 = q_base64;
          found_jpeg = true;
          break;
        }
      }

      if (found_jpeg) {
        const decoded_best = Buffer.from(best_base64!, "base64");
        return {
          resized_data: new Uint8Array(decoded_best),
          width: current_width,
          height: current_height,
          original_width,
          original_height,
          was_resized: true,
        };
      }

      if (current_width === 1 && current_height === 1) {
        break;
      }

      let next_w = Math.floor((current_width * 3) / 4);
      if (next_w < 1) next_w = 1;
      let next_h = Math.floor((current_height * 3) / 4);
      if (next_h < 1) next_h = 1;

      if (next_w === current_width && next_h === current_height) {
        break;
      }

      current_width = next_w;
      current_height = next_h;
    }

    return yield* Effect.fail(
      image_error("could not resize image below maxBytes limit", null),
    );
  });

export const format_dimension_note = (
  original_width: number,
  original_height: number,
  width: number,
  height: number,
  was_resized: boolean,
): string => {
  if (!was_resized) {
    return "";
  }
  const scale = original_width / width;
  return `[Image: original ${original_width}x${original_height}, displayed at ${width}x${height}. Multiply coordinates by ${scale.toFixed(2)} to map to original image.]`;
};

/*
PORT STATUS
source path: backend/imageutil/resize.go
source lines: 156
draft lines: 189
confidence: medium
status: phase_a_draft
todos:
  - replace decode_image stub with real PNG/JPEG/GIF/WEBP decoder
  - replace encode_png / encode_jpeg stubs with real encoders
  - implement bilinear scaling instead of using decoded_img as-is
  - decide whether to keep ImageData as the internal image representation
notes:
  - ResizeImage returns (T, error) in Go, modeled as Effect.Effect<resize_image_result, image_error>.
  - Original size/b64 limit logic preserved; only codec calls are stubbed.
*/
