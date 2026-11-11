# In-panel updates

When a new Veilon release is published, a banner appears at the top of the panel. **Install update** shows the release notes and then:

1. downloads the new binary,
2. verifies its checksum,
3. backs up the program and the database,
4. replaces the program,
5. restarts the panel,
6. waits up to 30 seconds for a healthy start.

If the new version is not healthy, the previous binary is restored automatically and the panel keeps running. The panel is unreachable for a few seconds during the restart; keep the page open.

The **Panel update** page (in the **More** menu, sudo admins only) shows the installed version, when the panel last looked for a release and when it looks next, and a **Check for updates** button. A new release shows its notes and an **Update** button there too. The panel also checks by itself every 6 hours.

## Servers on v0.2.2 or older

In-panel updates need a small helper service that older installs do not have. Run the install command once on the server; it upgrades the panel to the latest release, adds the helper and keeps all data and settings:

```bash
bash <(curl -Ls https://raw.githubusercontent.com/veilonhq/veilon/main/installer/install.sh)
```

Or run `sudo veilon update` twice: the first run installs the new release, the second adds the helper.

After that, every later update can be installed from the panel. `veilon update` on the server keeps working as before.

## Notes

- Rollback restores the program only. The database backup is kept on the server (its path is shown in the dialog) for a manual restore if ever needed.
