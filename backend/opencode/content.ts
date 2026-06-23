// PORT: backend/opencode/content.go

import { blocks_content, string_content, type content_block, type content_image_url, type json_raw_message } from "./types";

// ImagePart represents the mime type and raw bytes of an image to be attached.
export type image_part = { mime_type: string; data: Uint8Array };

// ToolContentBlock is shared across agent, verifier, and swarm.
export type tool_content_block = { type: string; text: string; data: string; mime_type: string };

// UserContent builds user content blocks from text and image parts.
export const user_content = (text: string, images: image_part[]): json_raw_message => {
  if (images.length === 0) return string_content(text);
  const blocks: content_block[] = [];
  if (text !== "") blocks.push({ type: "text", text });
  for (const img of images) {
    const encoded = Buffer.from(img.data).toString("base64");
    const data_url = `data:${img.mime_type};base64,${encoded}`;
    blocks.push({ type: "image_url", image_url: { url: data_url } as content_image_url });
  }
  return blocks_content(blocks);
};

// ToolContentFromAgent maps internal agent-style ToolContentBlocks to opencode ContentBlocks.
export const tool_content_from_agent = (blocks: tool_content_block[]): json_raw_message | null => {
  if (blocks.length === 0) return null;
  const out: content_block[] = [];
  for (const block of blocks) {
    if (block.type === "text") {
      out.push({ type: "text", text: block.text });
    } else if (block.type === "image") {
      out.push({ type: "image_url", image_url: { url: `data:${block.mime_type};base64,${block.data}` } });
    }
  }
  return blocks_content(out);
};

/*
PORT STATUS
source path: backend/opencode/content.go
source lines: 70
draft lines: 49
confidence: high
status: phase_a_draft
todos:
  - none
notes:
  - Pure JSON content helpers; no (T, error) returns.
*/
