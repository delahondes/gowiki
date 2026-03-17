# Images & Media

## 1. Uploading files

1. Click **Media** in the action bar to open the media manager
2. Select a file from your computer or drag and drop it
3. The file is uploaded to the current page's namespace

Alternatively, drag and drop an image directly onto the visual editor.

## 1. Inserting images

In visual mode, use the toolbar **Image** button or drag and drop.

In raw mode:

```
![Alt text](./image.png)
```

## 1. Image sizing

Drag the resize handles in visual mode to change the image size. Hold **Shift** to constrain proportions.

The size is stored as a directive:

```
{image size=400px}
![Alt text](./image.png)
```

## 1. Attachment links

Link to any file with an extension to create a download link:

```
[Download the report](./report.pdf)
```

The file type is detected automatically and displayed with an appropriate icon.

## 1. Where files are stored

Media files live alongside pages in `data/content/`. A file at `/regulatory/qms/dir/diagram.png` is stored at `content/regulatory/qms/dir/diagram.png`.

## 1. Orphan detection

When no page references a media file, the wiki detects it as an orphan. Admins can view orphaned media in the media manager.
