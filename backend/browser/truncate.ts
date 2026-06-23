// PORT: backend/browser/truncate.go

export const MaxScrapeBytes = 50 * 1024;
export const MaxScrapeLines = 2000;

export function truncateScrape(text: string): [string, boolean] {
  const totalBytes = Buffer.byteLength(text, "utf-8");

  let lines = text.split("\n");
  if (lines.length > 0 && text !== "" && text.endsWith("\n")) {
    lines = lines.slice(0, -1);
  }
  const totalLines = lines.length;

  if (totalLines <= MaxScrapeLines && totalBytes <= MaxScrapeBytes) {
    return [text, false];
  }

  const outputLines: string[] = [];
  let outputBytesCount = 0;
  let lastLinePartial = false;

  for (let i = lines.length - 1; i >= 0 && outputLines.length < MaxScrapeLines; i--) {
    const line = lines[i];
    let lineBytes = Buffer.byteLength(line, "utf-8");
    if (outputLines.length > 0) {
      lineBytes += 1; // +1 for newline
    }

    if (outputBytesCount + lineBytes > MaxScrapeBytes) {
      if (outputLines.length === 0) {
        const truncatedLine = truncateStringToBytesFromEnd(line, MaxScrapeBytes);
        outputLines.push(truncatedLine);
        lastLinePartial = true;
      }
      break;
    }

    outputLines.unshift(line);
    outputBytesCount += lineBytes;
  }

  const _ = lastLinePartial;
  return [outputLines.join("\n"), true];
}

export function truncateStringToBytesFromEnd(str: string, maxBytes: number): string {
  const buf = Buffer.from(str, "utf-8");
  if (buf.length <= maxBytes) {
    return str;
  }
  let start = buf.length - maxBytes;
  while (start < buf.length && (buf[start] & 0xC0) === 0x80) {
    start++;
  }
  return buf.subarray(start).toString("utf-8");
}

/*
PORT STATUS
source path: backend/browser/truncate.go
source lines: 63
draft lines: 59
confidence: high
status: phase_b_compile
*/
