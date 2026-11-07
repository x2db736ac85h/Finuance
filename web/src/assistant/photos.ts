// Photos for the assistant (Phase 20, receipts by photo). A photo is re-encoded in the browser
// before it leaves the device: drawn onto a canvas at most MAX_SIDE px a side and saved as a JPEG,
// so the camera's EXIF data (location, device, time) never reaches the server. The server checks
// and re-encodes it again.

/** The longest side a photo is sent with (receipts stay readable). */
export const MAX_SIDE = 1600;
const QUALITY = 0.85;

/** The size a photo is drawn at: its own, or scaled down so the longer side is `max`. */
export function fitWithin(w: number, h: number, max = MAX_SIDE): { w: number; h: number } {
  const s = Math.min(1, max / Math.max(w, h));
  return { w: Math.max(1, Math.round(w * s)), h: Math.max(1, Math.round(h * s)) };
}

/** Re-encodes a picked photo (any format the browser can show) to a JPEG without metadata. */
export async function reencodePhoto(file: Blob): Promise<Blob> {
  // the EXIF orientation is applied when decoding, then dropped with the rest of the metadata
  const bmp = await createImageBitmap(file, { imageOrientation: 'from-image' });
  const { w, h } = fitWithin(bmp.width, bmp.height);
  const canvas = document.createElement('canvas');
  canvas.width = w;
  canvas.height = h;
  const g = canvas.getContext('2d');
  if (!g) throw new Error('This browser cannot prepare photos.');
  g.drawImage(bmp, 0, 0, w, h);
  bmp.close();
  return new Promise((resolve, reject) => {
    canvas.toBlob(
      (b) => {
        if (b) resolve(b);
        else reject(new Error('This browser cannot prepare photos.'));
      },
      'image/jpeg',
      QUALITY,
    );
  });
}
