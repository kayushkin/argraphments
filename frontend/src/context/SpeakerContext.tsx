import React, { createContext, useContext, useState, useCallback, useRef } from 'react';
import { speakerBgColor as calcColor } from '../utils/colors';
import { renameSpeakerAPI, updateTranscriptSpeakers } from '../api';

const ANON_NAMES = [
  'Alex','Blake','Casey','Dana','Eden','Finn','Gray','Harper',
  'Ivy','Jay','Kit','Lane','Morgan','Noel','Oak','Parker',
  'Quinn','Ray','Sam','Tate','Val','Wren','Zara','Sage',
  'Ash','Brook','Drew','Ellis','Fern','Glen','Haven','Jade',
  'Kai','Lark','Maple','Nico','Olive','Pax','Reed','Sky',
];

function isDefaultSpeakerName(name: string): boolean {
  return /^speaker[_ ]\d+$/i.test(name);
}

interface SpeakerContextValue {
  speakerNames: Record<string, string>;
  speakerAutoGen: Record<string, boolean>;
  speakerDbIds: Record<string, number>;
  setSpeakerNames: React.Dispatch<React.SetStateAction<Record<string, string>>>;
  setSpeakerAutoGen: React.Dispatch<React.SetStateAction<Record<string, boolean>>>;
  setSpeakerDbIds: React.Dispatch<React.SetStateAction<Record<string, number>>>;
  resetSpeakers: () => void;
  pickAnonName: () => string;
  initSpeakersFromDiarize: (speakers: Record<string, string>) => void;
  renameSpeaker: (id: string, newName: string, slug?: string) => void;
  resolveSpeaker: (raw: string) => string;
  getSpeakerColor: (speaker: string) => string;
}

const SpeakerContext = createContext<SpeakerContextValue>(null!);
export const useSpeakers = () => useContext(SpeakerContext);

export function SpeakerProvider({ children }: { children: React.ReactNode }) {
  const [speakerNames, setSpeakerNames] = useState<Record<string, string>>({});
  const [speakerAutoGen, setSpeakerAutoGen] = useState<Record<string, boolean>>({});
  const [speakerDbIds, setSpeakerDbIds] = useState<Record<string, number>>({});
  const usedAnonNames = useRef(new Set<string>());

  const pickAnonName = useCallback(() => {
    const available = ANON_NAMES.filter((n) => !usedAnonNames.current.has(n));
    const name =
      available.length > 0
        ? available[Math.floor(Math.random() * available.length)]
        : ANON_NAMES[Math.floor(Math.random() * ANON_NAMES.length)] +
          Math.floor(Math.random() * 99);
    usedAnonNames.current.add(name);
    return name;
  }, []);

  const resetSpeakers = useCallback(() => {
    setSpeakerNames({});
    setSpeakerAutoGen({});
    setSpeakerDbIds({});
    usedAnonNames.current.clear();
  }, []);

  const initSpeakersFromDiarize = useCallback(
    (speakers: Record<string, string>) => {
      setSpeakerNames((prev) => {
        const next = { ...prev };
        setSpeakerAutoGen((prevAuto) => {
          const nextAuto = { ...prevAuto };
          for (const [id, name] of Object.entries(speakers)) {
            if (!next[id]) {
              if (!name || isDefaultSpeakerName(name)) {
                const anon = pickAnonName();
                next[id] = anon;
                nextAuto[id] = true;
              } else {
                next[id] = name;
              }
            } else if (name && !isDefaultSpeakerName(name) && prevAuto[id]) {
              next[id] = name;
              nextAuto[id] = false;
            }
          }
          return nextAuto;
        });
        return next;
      });
    },
    [pickAnonName]
  );

  const renameSpeaker = useCallback(
    (id: string, newName: string, slug?: string) => {
      setSpeakerNames((prev) => {
        const oldName = prev[id];
        const next = { ...prev, [id]: newName };
        // Persist
        if (oldName && oldName !== newName) {
          renameSpeakerAPI(oldName, newName).catch(() => {});
        }
        if (slug) {
          setSpeakerAutoGen((prevAuto) => {
            const nextAuto = { ...prevAuto, [id]: false };
            updateTranscriptSpeakers(slug, next, nextAuto).catch(() => {});
            return nextAuto;
          });
        } else {
          setSpeakerAutoGen((prevAuto) => ({ ...prevAuto, [id]: false }));
        }
        return next;
      });
    },
    []
  );

  const resolveSpeaker = useCallback(
    (raw: string): string => {
      if (!raw) return '';
      for (const [id, name] of Object.entries(speakerNames)) {
        if (id === raw || name === raw) return name;
      }
      const normalized = raw.toLowerCase().replace(/\s+/g, '_');
      if (speakerNames[normalized]) return speakerNames[normalized];
      return raw;
    },
    [speakerNames]
  );

  const getSpeakerColor = useCallback(
    (speaker: string) => calcColor(speaker, speakerNames),
    [speakerNames]
  );

  return (
    <SpeakerContext.Provider
      value={{
        speakerNames,
        speakerAutoGen,
        speakerDbIds,
        setSpeakerNames,
        setSpeakerAutoGen,
        setSpeakerDbIds,
        resetSpeakers,
        pickAnonName,
        initSpeakersFromDiarize,
        renameSpeaker,
        resolveSpeaker,
        getSpeakerColor,
      }}
    >
      {children}
    </SpeakerContext.Provider>
  );
}
