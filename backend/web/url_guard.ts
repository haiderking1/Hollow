// PORT: backend/web/url_guard.go

import dns from "node:dns/promises";

export const is_http_url = (s: string): boolean => {
  try {
    const u = new URL(s);
    return u.protocol === "http:" || u.protocol === "https:";
  } catch {
    return false;
  }
};

export const validate_fetch_url = async (raw: string): Promise<URL> => {
  let u: URL;
  try {
    u = new URL(raw);
  } catch (err: any) {
    throw new Error(`invalid url: ${err.message}`);
  }

  if (u.protocol !== "http:" && u.protocol !== "https:") {
    throw new Error("only http and https urls are allowed");
  }
  if (u.host === "") {
    throw new Error("url missing host");
  }

  const hostname = u.hostname;
  if (hostname.toLowerCase() === "localhost" && !allow_private_fetch()) {
    throw new Error("localhost urls are not allowed");
  }

  try {
    const ips = await dns.lookup(hostname, { all: true });
    if (!allow_private_fetch()) {
      for (const ipInfo of ips) {
        if (is_private_ip(ipInfo.address)) {
          throw new Error("private network urls are not allowed");
        }
      }
    }
  } catch (err: any) {
    if (err.message === "private network urls are not allowed") {
      throw err;
    }
  }
  return u;
};

export const allow_private_fetch = (): boolean => {
  return process.env.HOLLOW_WEB_ALLOW_PRIVATE === "1";
};

export const is_private_ip = (ip: string): boolean => {
  if (ip === "127.0.0.1" || ip === "::1" || ip.startsWith("fe80:") || ip.startsWith("ff02:")) {
    return true;
  }

  const ipv4Parts = ip.split(".");
  if (ipv4Parts.length === 4) {
    const octet0 = parseInt(ipv4Parts[0], 10);
    const octet1 = parseInt(ipv4Parts[1], 10);
    if (isNaN(octet0) || isNaN(octet1)) return false;

    if (octet0 === 10) return true;
    if (octet0 === 172 && octet1 >= 16 && octet1 <= 31) return true;
    if (octet0 === 192 && octet1 === 168) return true;
    if (octet0 === 127) return true;
    if (octet0 === 169 && octet1 === 254) return true;
    if (octet0 === 0) return true;
  }

  return false;
};

export const user_agent = (): string => {
  return "Hollow/1.0 (+https://github.com/haiderking1/Hollow)";
};

/*
PORT STATUS
source path: backend/web/url_guard.go
source lines: 81
confidence: high
status: phase_b_compile
*/
