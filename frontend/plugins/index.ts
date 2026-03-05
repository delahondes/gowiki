import type { Plugin } from "../compiler/registry"
import { imagePlugin } from "./image"
import { blockquotePlugin } from "./blockquote"
import { tablePlugin } from "./table"
import { includePlugin } from "./include"
import { medialinkPlugin } from "./medialink"
import { databasePlugin } from "./database"
import { tagPlugin } from "./tag"
import { todoPlugin } from "./todo"

export const plugins: Plugin[] = [
  imagePlugin,
  blockquotePlugin,
  tablePlugin,
  includePlugin,
  medialinkPlugin,
  databasePlugin,
  tagPlugin,
  todoPlugin,
]
