import type { Plugin } from "../compiler/registry"
import { blockquotePlugin } from "./blockquote"
import { tablePlugin } from "./table"

export const plugins: Plugin[] = [
  blockquotePlugin,
  tablePlugin
]