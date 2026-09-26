// {todo ...} + {todo-list ...} directives — frontend serialization side.
// The backend has its own suite for the actual todo lifecycle
// (backend/internal/todo/*); here we pin directive round-trip and the
// bijective attr encoding.
import { describe, it, expect } from "vitest"
import type { Node as PMNode } from "prosemirror-model"
import { roundTrip, countNodes } from "../helpers"

function firstTodo(doc: PMNode): Record<string, unknown> | null {
  let found: Record<string, unknown> | null = null
  doc.descendants((n) => {
    if (found === null && n.type.name === "todo") {
      found = { ...n.attrs }
      return false
    }
    return true
  })
  return found
}

function firstTodoList(doc: PMNode): Record<string, unknown> | null {
  let found: Record<string, unknown> | null = null
  doc.descendants((n) => {
    if (found === null && n.type.name === "todo_list") {
      found = { ...n.attrs }
      return false
    }
    return true
  })
  return found
}

describe("todo: minimum shape", () => {
  it('{todo title="Ship v1"} produces one todo node', () => {
    const rt = roundTrip('{todo title="Ship v1"}\n')
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "todo")).toBe(1)
    expect(firstTodo(rt.doc)?.title).toBe("Ship v1")
  })

  it("title is always emitted even when empty", () => {
    // The serializer emits title="" unconditionally — pinning that
    // invariant here so a future "optimisation" that drops it can't
    // slip past.
    const rt = roundTrip('{todo title=""}\n')
    expect(rt.isStable).toBe(true)
    expect(rt.first).toMatch(/title=""/)
  })
})

describe("todo: individual attrs", () => {
  it("assign=", () => {
    const rt = roundTrip('{todo title="X" assign="@alice"}\n')
    expect(rt.isStable).toBe(true)
    expect(firstTodo(rt.doc)?.assign).toBe("@alice")
  })

  it("due=", () => {
    const rt = roundTrip('{todo title="X" due=2026-12-31}\n')
    expect(rt.isStable).toBe(true)
    expect(firstTodo(rt.doc)?.due).toBe("2026-12-31")
  })

  it("recur=", () => {
    const rt = roundTrip('{todo title="X" recur=weekly}\n')
    expect(rt.isStable).toBe(true)
    expect(firstTodo(rt.doc)?.recur).toBe("weekly")
  })

  it("priority=high (non-default)", () => {
    const rt = roundTrip('{todo title="X" priority=high}\n')
    expect(rt.isStable).toBe(true)
    expect(firstTodo(rt.doc)?.priority).toBe("high")
  })

  it("priority=normal (default) is dropped on serialize", () => {
    const rt = roundTrip('{todo title="X" priority=normal}\n')
    expect(rt.isStable).toBe(true)
    expect(rt.first).not.toMatch(/priority=normal/)
  })

  it("resolution=all (non-default)", () => {
    const rt = roundTrip('{todo title="X" resolution=all}\n')
    expect(rt.isStable).toBe(true)
    expect(firstTodo(rt.doc)?.resolution).toBe("all")
  })

  it("resolution=any (default) is dropped on serialize", () => {
    const rt = roundTrip('{todo title="X" resolution=any}\n')
    expect(rt.isStable).toBe(true)
    expect(rt.first).not.toMatch(/resolution=any/)
  })

  it("action= carries a comma-separated action list", () => {
    const rt = roundTrip('{todo title="X" action="review,approve"}\n')
    expect(rt.isStable).toBe(true)
    expect(firstTodo(rt.doc)?.action).toBe("review,approve")
  })

  it("tags=", () => {
    const rt = roundTrip('{todo title="X" tags="regulatory,urgent"}\n')
    expect(rt.isStable).toBe(true)
    expect(firstTodo(rt.doc)?.tags).toBe("regulatory,urgent")
  })

  it("description= with special chars in quoted value", () => {
    const rt = roundTrip('{todo title="X" description="See section 3.1 & attached memo"}\n')
    expect(rt.isStable).toBe(true)
    expect(firstTodo(rt.doc)?.description).toBe("See section 3.1 & attached memo")
  })
})

describe("todo: combined attrs", () => {
  it("full combination round-trips", () => {
    const rt = roundTrip(
      '{todo title="Ship v1" assign="@alice" due=2026-12-31 recur=weekly priority=high tags="rel"}\n'
    )
    expect(rt.isStable).toBe(true)
    const a = firstTodo(rt.doc)!
    expect(a.title).toBe("Ship v1")
    expect(a.assign).toBe("@alice")
    expect(a.due).toBe("2026-12-31")
    expect(a.recur).toBe("weekly")
    expect(a.priority).toBe("high")
    expect(a.tags).toBe("rel")
  })
})

describe("todo-list: minimum shape", () => {
  it("bare {todo-list} produces one node with defaults", () => {
    const rt = roundTrip("{todo-list}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "todo_list")).toBe(1)
    const a = firstTodoList(rt.doc)!
    expect(a.status).toBe("open,in_progress")
    expect(a.limit).toBe("20")
  })

  it("bare {todo-list} does NOT re-emit default status/limit", () => {
    const rt = roundTrip("{todo-list}\n")
    expect(rt.first).not.toMatch(/status=/)
    expect(rt.first).not.toMatch(/limit=/)
  })
})

describe("todo-list: individual attrs", () => {
  it("assign=", () => {
    const rt = roundTrip('{todo-list assign="@alice"}\n')
    expect(rt.isStable).toBe(true)
    expect(firstTodoList(rt.doc)?.assign).toBe("@alice")
  })

  it("status= (non-default)", () => {
    const rt = roundTrip('{todo-list status="done"}\n')
    expect(rt.isStable).toBe(true)
    expect(firstTodoList(rt.doc)?.status).toBe("done")
  })

  it("priority=", () => {
    const rt = roundTrip('{todo-list priority="high"}\n')
    expect(rt.isStable).toBe(true)
    expect(firstTodoList(rt.doc)?.priority).toBe("high")
  })

  it("tag=", () => {
    const rt = roundTrip('{todo-list tag="regulatory"}\n')
    expect(rt.isStable).toBe(true)
    expect(firstTodoList(rt.doc)?.tag).toBe("regulatory")
  })

  it("due_before=", () => {
    const rt = roundTrip("{todo-list due_before=2026-12-31}\n")
    expect(rt.isStable).toBe(true)
    expect(firstTodoList(rt.doc)?.due_before).toBe("2026-12-31")
  })

  it("limit=100 (non-default)", () => {
    const rt = roundTrip("{todo-list limit=100}\n")
    expect(rt.isStable).toBe(true)
    expect(firstTodoList(rt.doc)?.limit).toBe("100")
  })
})

describe("todo: interaction with code fences", () => {
  it("{todo} inside a fenced block stays literal", () => {
    const src = '```\n{todo title="X"}\n```\n'
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "todo")).toBe(0)
  })

  it("{todo-list} inside a backtick-protected cell stays literal", () => {
    const src = '| head |\n| --- |\n| `{todo-list assign="@alice"}` |\n'
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "todo_list")).toBe(0)
  })
})
