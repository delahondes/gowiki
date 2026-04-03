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
import { reviewflowLinkPlugin } from "./reviewflowlink"
import { versionLinkPlugin } from "./versionlink"
import { changesPlugin } from "./changes"
import { commentPlugin } from "./comment"
import { spoilerPlugin } from "./spoiler"
import { chartPlugin } from "./chart"
import { mermaidPlugin } from "./mermaid"
import { slidePlugin } from "./slide"
import { captionPlugin } from "./caption"
import { footnotePlugin } from "./footnote"
import { highlightPlugin } from "./highlight"
import { flowMarkerPlugin } from "./flow_marker"

export const plugins: Plugin[] = [
  captionPlugin,
  footnotePlugin,
  highlightPlugin,
  imagePlugin,
  blockquotePlugin,
  tablePlugin,
  includePlugin,
  medialinkPlugin,
  databasePlugin,
  tagPlugin,
  todoPlugin,
  reviewflowPlugin,
  reviewflowLinkPlugin,
  versionLinkPlugin,
  changesPlugin,
  commentPlugin,
  spoilerPlugin,
  chartPlugin,
  mermaidPlugin,
  slidePlugin,
  flowMarkerPlugin,
]
