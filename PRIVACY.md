# Privacy policy — Safe Idealista

Last updated: 2026-10-22

Safe Idealista is a browser extension that shows neighbourhood-quality indicators and the distance to your own address on Idealista listings. It has no account, no analytics, no ads and no server of its own.

## What the extension stores

All of it stays in your browser:

- **Settings** (`chrome.storage.sync`): the on/off switch, language, which layers are shown, their weights, display options and, if you set one, your home address with its coordinates. If Chrome sync is on, Chrome syncs these settings between your own browsers. The developer cannot access them.
- **Data updates** (`chrome.storage.local`): the downloaded neighbourhood dataset and the time of the last update check.

Uninstalling the extension deletes all of it.

## What leaves your browser

- **Your home address, only when you set it.** When you type an address in the popup and press Set, the extension sends that text to [OpenStreetMap Nominatim](https://nominatim.openstreetmap.org) to turn it into coordinates. No other data goes with it. If you enter coordinates (for example `41.3925, 2.1640`), nothing is sent. Nominatim is run by the OpenStreetMap Foundation, see its [privacy policy](https://osmfoundation.org/wiki/Privacy_Policy).
- **Data update checks.** Once a day the extension downloads a public `release.json` file, and any changed data files, from `raw.githubusercontent.com` (GitHub). These are plain file downloads with no personal data in them. GitHub sees the request as it would for any web request, see the [GitHub privacy statement](https://docs.github.com/site-policy/privacy-policies/github-general-privacy-statement).

## What the extension does not do

- It reads Idealista pages only to find where each listing is and to draw its badge. It does not record, store or send what you browse.
- It does not collect, sell or share personal data, and does not use it for advertising, credit decisions or any purpose other than the extension's single purpose.
- It does not load or run remote code. Downloaded files are JSON data, checked against SHA-256 hashes.

## Contact

Questions: open an issue at <https://github.com/insigmo/safe_idealista/issues>.
