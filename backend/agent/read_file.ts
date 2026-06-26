import { Effect } from "effect";
import fs from "node:fs/promises";
import { type tool } from "../opencode/types";
import { Agent, type toolResult } from "./agent";
import { detect_supported_image_mime_type } from "../imageutil/mime";
import { resize_image } from "../imageutil/resize";
import { supports_images } from "../opencode/models";

export function readFileTool(): tool {
  const schema = {
    type: "object",
    properties: {
      path: { type: "string", description: "Relative or absolute path" },
    },
    required: ["path"],
  };
  return {
    type: "function",
    function: {
      name: "read_file",
      description: "Read the contents of a file. Supports text files and images (jpg, png, gif, webp). Images are sent as attachments. For text files, output is truncated.",
      parameters: new TextEncoder().encode(JSON.stringify(schema)),
    },
  };
}

Agent.prototype.toolReadFile = function (
  this: Agent,
  argsJSON: string
): Effect.Effect<toolResult, Error> {
  return Effect.gen(this, function* () {
    let args: { path: string };
    try {
      args = JSON.parse(argsJSON);
    } catch (err) {
      return { output: err instanceof Error ? err.message : String(err), isErr: true };
    }

    const resolvedPath = yield* this.resolvePath(args.path);

    const data = yield* Effect.tryPromise({
      try: async () => await fs.readFile(resolvedPath),
      catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
    });

    const mimeType = detect_supported_image_mime_type(data);
    if (mimeType !== "") {
      const supportsVision = supports_images(this.cfg.model);
      
      const resizeResult = yield* resize_image(data, mimeType).pipe(
        Effect.either,
        Effect.map((res) => {
          if (res._tag === "Left") {
            let textNote = `Read image file [${mimeType}]\n[Image omitted: could not be resized below the inline image size limit.]`;
            if (!supportsVision) {
              textNote += "\n[This session does not support images. The image will be omitted from this request.]";
            }
            return {
              output: textNote,
              content: [
                { type: "text", text: textNote, data: "", mime_type: "" },
              ],
            } as toolResult;
          }
          
          const r = res.right;
          let textNote = `Read image file [${mimeType}]\n${r.width}×${r.height}`;
          if (r.was_resized) {
            const scale = r.original_width / r.width;
            textNote += `\n[Image: original ${r.original_width}x${r.original_height}, displayed at ${r.width}x${r.height}. Multiply coordinates by ${scale.toFixed(2)} to map to original image.]`;
          }
          if (!supportsVision) {
            textNote += "\n[This session does not support images. The image will be omitted from this request.]";
          }

          const contentBlocks = [
            { type: "text", text: textNote, data: "", mime_type: "" },
          ];
          if (supportsVision) {
            contentBlocks.push({
              type: "image",
              text: "",
              data: Buffer.from(r.resized_data).toString("base64"),
              mime_type: mimeType,
            });
          }

          return {
            output: textNote,
            content: contentBlocks,
          } as toolResult;
        })
      );

      return resizeResult;
    }

    const max = 64000;
    let out = data.toString("utf8");
    let truncated = false;
    if (out.length > max) {
      out = out.slice(0, max);
      truncated = true;
    }

    let lines = 0;
    // Count newlines in raw data to keep the total accurate
    const rawStr = data.toString("utf8");
    for (let i = 0; i < rawStr.length; i++) {
      if (rawStr[i] === "\n") {
        lines++;
      }
    }
    if (data.length > 0 && !rawStr.endsWith("\n")) {
      lines++;
    }

    let header = `Read ${lines} lines from ${resolvedPath}\n`;
    if (truncated) {
      out += "\n... truncated ...";
    }
    return {
      output: header + out,
    };
  }).pipe(
    Effect.catchAll((err) =>
      Effect.succeed({ output: err.message, isErr: true })
    )
  );
};

