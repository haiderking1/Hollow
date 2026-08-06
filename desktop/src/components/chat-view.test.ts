import { describe, expect, it } from "bun:test"
import {
  BOTTOM_THRESHOLD_PX,
  computeNextPinnedState,
  isNearBottomCalc,
  isScrollingUp,
} from "./chat-view"

describe("chat auto-scroll behavior", () => {
  it("correctly calculates near-bottom threshold", () => {
    // Exactly at bottom
    expect(isNearBottomCalc(1000, 600, 400)).toBe(true)
    // Within 80px threshold (50px distance to bottom)
    expect(isNearBottomCalc(1000, 550, 400)).toBe(true)
    // Exactly at 80px threshold
    expect(isNearBottomCalc(1000, 520, 400)).toBe(true)
    // Beyond threshold (100px distance to bottom)
    expect(isNearBottomCalc(1000, 500, 400)).toBe(false)
  })

  it("detects upward scrolling intent accurately while ignoring subpixel jitter", () => {
    // Clearly scrolling up (from 600 to 550)
    expect(isScrollingUp(550, 600)).toBe(true)
    // Scrolling down (from 500 to 550)
    expect(isScrollingUp(550, 500)).toBe(false)
    // Stationary (no change)
    expect(isScrollingUp(550, 550)).toBe(false)
    // Subpixel rounding noise (e.g. 0.2px)
    expect(isScrollingUp(549.8, 550)).toBe(false)
  })

  it("unpins auto-scroll immediately on explicit upward user wheel gesture", () => {
    const nextState = computeNextPinnedState({
      currentScrollTop: 600,
      lastScrollTop: 600,
      scrollHeight: 1000,
      clientHeight: 400,
      wheelUp: true,
    })
    expect(nextState).toBe(false)
  })

  it("unpins auto-scroll when user intentionally scrolls upward", () => {
    const nextState = computeNextPinnedState({
      currentScrollTop: 550,
      lastScrollTop: 600,
      scrollHeight: 1000,
      clientHeight: 400,
    })
    expect(nextState).toBe(false)
  })

  it("remains unpinned when user is scrolled up far from bottom", () => {
    const nextState = computeNextPinnedState({
      currentScrollTop: 200,
      lastScrollTop: 200,
      scrollHeight: 1000,
      clientHeight: 400,
    })
    expect(nextState).toBe(false)
  })

  it("repins auto-scroll when user scrolls down near the bottom threshold", () => {
    const nextState = computeNextPinnedState({
      currentScrollTop: 550,
      lastScrollTop: 500,
      scrollHeight: 1000,
      clientHeight: 400,
    })
    expect(nextState).toBe(true)
  })
})
