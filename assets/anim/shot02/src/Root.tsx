import React from 'react';
import { Composition } from 'remotion';
import { Shot02 } from './Shot02';

export const Root: React.FC = () => (
  <Composition
    id="Shot02"
    component={Shot02}
    durationInFrames={600}
    fps={30}
    width={1920}
    height={1080}
  />
);
