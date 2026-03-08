import type { Plugin } from "../compiler/registry"
import { imagePlugin } from "./image"
import { blockquotePlugin } from "./blockquote"
import { tablePlugin } from "./table"
import { includePlugin } from "./include"
import { medialinkPlugin } from "./medialink"
import { databasePlugin } from "./database"
import { tagPlugin } from "./tag"
import { todoPlugin } from "./todo"
import { reviewflowPlugin } from "./reviewflow"
import { changesPlugin } from "./changes"

export const plugins: Plugin[] = [
  imagePlugin,
  blockquotePlugin,
  tablePlugin,
  includePlugin,
  medialinkPlugin,
  databasePlugin,
  tagPlugin,
  todoPlugin,
  reviewflowPlugin,
  changesPlugin,
]
