// Dev-only: inject into an Idealista page (e.g. via a <script> tag pointing
// at a local server) to exercise the extension without installing it.
// window.__bcnsqSet({ hideRed: true }) changes settings like the popup does.
import './shim';
import css from '../src/content.css';
import { DEFAULTS, saveSettings, type Settings } from '../src/settings';
import '../src/page';
import '../src/content';

const style = document.createElement('style');
style.textContent = css;
document.head.append(style);

let current: Settings = structuredClone(DEFAULTS);
(window as unknown as { __bcnsqSet: (p: Partial<Settings>) => Promise<void> }).__bcnsqSet = (patch) => {
  current = { ...current, ...patch };
  return saveSettings(current);
};
