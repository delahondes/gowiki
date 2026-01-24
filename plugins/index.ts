import type { Plugin } from "../compiler/registry"
import { blockquotePlugin } from "./blockquote"

export const plugins: Plugin[] = [
  blockquotePlugin,
]