export const speakerColors = ['#7c6ff0', '#6ec1e4', '#e4c76e', '#7ce4a1', '#e47070', '#b070e4'];

export function getColorMap(speakerNames: Record<string, string>): Record<string, string> {
  const ids = Object.keys(speakerNames);
  const map: Record<string, string> = {};
  ids.forEach((id, i) => {
    map[id] = speakerColors[i % speakerColors.length];
  });
  return map;
}

export function speakerIdFromName(
  raw: string,
  speakerNames: Record<string, string>
): string | null {
  if (!raw) return null;
  const lower = raw.toLowerCase().trim();
  for (const [id, name] of Object.entries(speakerNames)) {
    if (id === raw || name === raw) return id;
    if (id === lower || name.toLowerCase() === lower) return id;
  }
  return null;
}

export function speakerBgColor(
  speaker: string,
  speakerNames: Record<string, string>
): string {
  const colorMap = getColorMap(speakerNames);
  if (colorMap[speaker]) return colorMap[speaker];
  const id = speakerIdFromName(speaker, speakerNames);
  if (id && colorMap[id]) return colorMap[id];
  const s = (speaker || '').toLowerCase();
  const idx = Array.from(s).reduce((a, c) => a + c.charCodeAt(0), 0);
  return speakerColors[idx % speakerColors.length];
}
