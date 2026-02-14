import type { Plugin } from "../compiler/registry"
import { imagePlugin } from "./image"
import { blockquotePlugin } from "./blockquote"
import { tablePlugin } from "./table"

export const plugins: Plugin[] = [
  imagePlugin,
  blockquotePlugin,
  tablePlugin,
]
