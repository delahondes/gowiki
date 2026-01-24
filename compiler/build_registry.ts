import { Schema } from "prosemirror-model"
import { Registry } from "./registry"
import { registerCoreNodes } from "./core_nodes"
import { plugins } from "../plugins"

/**
 * Build and fully configure a Registry for this schema.
 */
export function buildRegistry(schema: Schema): Registry {
  const reg = new Registry(schema)

  // core language
  registerCoreNodes(reg)

  // plugins (uniform, no explicit calls)
  for (const plugin of plugins) {
    plugin.register(reg)
  }

  return reg
}