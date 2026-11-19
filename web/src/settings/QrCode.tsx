import { encodeQr, qrPath } from '../lib/qr';

/**
 * A QR code of `text` as an SVG, always dark on white with the quiet zone scanners need, whatever
 * the colour theme. Renders nothing when the text is too long for the encoder (lib/qr.ts); the
 * caller shows the text itself beside it either way.
 */
export function QrCode({ text, label, size = 220 }: { text: string; label: string; size?: number }) {
  const qr = encodeQr(text);
  if (!qr) return null;
  const quiet = 4;
  const side = qr.size + 2 * quiet;
  return (
    <svg
      className="qr"
      role="img"
      aria-label={label}
      width={size}
      height={size}
      viewBox={`0 0 ${String(side)} ${String(side)}`}
      shapeRendering="crispEdges"
    >
      <rect width={side} height={side} fill="#fff" />
      <path d={qrPath(qr, quiet)} fill="#000" />
    </svg>
  );
}
