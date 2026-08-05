import { describe, expect, it } from "bun:test"
import { slashCommandKeyAction } from "./prompt-input"

describe("slash command keyboard behavior", () => {
  it("submits the selected command on the first Enter", () => {
    expect(slashCommandKeyAction("Enter", false, "/compact")).toEqual({
      value: "/compact",
      submit: true,
    })
  })

  it("completes with Tab without submitting", () => {
    expect(slashCommandKeyAction("Tab", false, "/compact")).toEqual({
      value: "/compact ",
      submit: false,
    })
  })

  it("leaves Shift+Enter available for a newline", () => {
    expect(slashCommandKeyAction("Enter", true, "/compact")).toBeNull()
  })
})
