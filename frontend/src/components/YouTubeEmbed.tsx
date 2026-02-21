import React from 'react';

interface Props {
  videoId: string;
  autoplay?: boolean;
}

export default function YouTubeEmbed({ videoId, autoplay }: Props) {
  const src = `https://www.youtube.com/embed/${videoId}${autoplay ? '?autoplay=1&enablejsapi=1' : ''}`;
  return (
    <div id="yt-embed-container">
      <div className="yt-embed-wrapper">
        <iframe
          src={src}
          frameBorder="0"
          allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture"
          allowFullScreen
        />
      </div>
    </div>
  );
}
