// Interpolation of `{{name}}` inside directive attributes.
//
// The public wrapper `expandTemplateVars` needs an EditorView to reach
// the global-var context and the current document's database-row
// fields. The core replacement logic is factored out into
// `interpolateVars` so it can be tested in isolation without mounting
// an editor — the wrapper's only job is to compose that core with the
// two live resolvers.
import { describe, it, expect } from "vitest"
import { interpolateVars } from "../../plugins/database"

describe("interpolateVars", () => {
  it("passes through strings with no {{...}}", () => {
    expect(interpolateVars("status=active", () => "unused")).toBe("status=active")
    expect(interpolateVars("", () => "unused")).toBe("")
  })

  it("substitutes a known name", () => {
    expect(interpolateVars("id={{id}}", (n) => (n === "id" ? "42" : undefined))).toBe("id=42")
  })

  it("substitutes multiple names in one string", () => {
    const resolve = (n: string) => ({ id: "42", status: "open" })[n]
    expect(interpolateVars("id={{id}}&status={{status}}", resolve)).toBe("id=42&status=open")
  })

  it("leaves unknown names LITERAL (typos surface in the request)", () => {
    // The whole point: silently substituting `""` would collapse to
    // `id=` which usually matches every row. Better to fail loudly.
    const resolve = (n: string) => (n === "id" ? "42" : undefined)
    expect(interpolateVars("id={{ide}}", resolve)).toBe("id={{ide}}")
    expect(interpolateVars("id={{id}}&x={{missing}}", resolve)).toBe("id=42&x={{missing}}")
  })

  it("passes empty-string resolution through (empty is intentional)", () => {
    const resolve = (n: string) => (n === "id" ? "" : undefined)
    expect(interpolateVars("id={{id}}", resolve)).toBe("id=")
  })

  it("tolerates whitespace inside the braces", () => {
    const resolve = (n: string) => (n === "id" ? "42" : undefined)
    expect(interpolateVars("id={{ id }}", resolve)).toBe("id=42")
    expect(interpolateVars("id={{  id  }}", resolve)).toBe("id=42")
  })

  it("does not touch a lone '{{' with no closing braces", () => {
    expect(interpolateVars("literal {{ unclosed", () => "X")).toBe("literal {{ unclosed")
  })

  it("does not recurse into a resolved value's own {{name}}", () => {
    // A resolver that returns `{{id}}` should NOT re-trigger interpolation.
    // The replacement is a single pass; we do not want a resolver bug or
    // hostile input to create an infinite substitution loop.
    const resolve = (n: string) => (n === "id" ? "{{other}}" : "SHOULD_NOT_APPEAR")
    expect(interpolateVars("value={{id}}", resolve)).toBe("value={{other}}")
  })

  it("only touches valid inner names (rejects empty {{}})", () => {
    // `{{}}` has no name — should stay literal, not call the resolver
    // with an empty string.
    const resolver = (n: string) => {
      throw new Error("resolver should not be called with " + JSON.stringify(n))
    }
    // `{{  }}` matches the regex with a whitespace-only capture; the
    // trimmed-empty guard inside interpolateVars leaves it literal
    // without hitting the resolver.
    expect(interpolateVars("x = {{ }}", resolver)).toBe("x = {{ }}")
  })

  it("substitutes at boundaries — start, middle, end of the string", () => {
    const resolve = (n: string) => "X"
    expect(interpolateVars("{{a}}yz", resolve)).toBe("Xyz")
    expect(interpolateVars("y{{a}}z", resolve)).toBe("yXz")
    expect(interpolateVars("yz{{a}}", resolve)).toBe("yzX")
  })

  it("handles a resolved value containing regex-special characters", () => {
    // The replacement value is used verbatim; String.replace's `$1` etc
    // magic would corrupt values containing `$`. Verify we're not hit
    // by that class of bug.
    const resolve = (n: string) => (n === "raw" ? "$1$2$&" : undefined)
    expect(interpolateVars("out={{raw}}", resolve)).toBe("out=$1$2$&")
  })
})
