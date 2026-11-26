{
  config,
  lib,
  pkgs,
  options,
  ...
}:

with lib;

let
  cfg = config.services.qwdtt;

  qwdtt-package = pkgs.buildGoModule {
    pname = "qwdtt";
    version = "1.1.6";

    src = ./../..;
    vendorHash = null;

    subPackages = [ "." ];
    ldflags = [ "-s" "-w" ];

    postInstall = ''
      mkdir -p $out/share/bash-completion/completions
      cp $src/completions/qwdtt.bash $out/share/bash-completion/completions/qwdtt

      mkdir -p $out/share/fish/vendor_completions.d
      cp $src/completions/qwdtt.fish $out/share/fish/vendor_completions.d/qwdtt.fish
    '';

    meta = {
      description = "VPN client через TURN-серверы VK";
      license = licenses.mit;
      maintainers = [ ];
    };
  };

  # ---------------------------------------------------------------------------
  # Helpers
  # ---------------------------------------------------------------------------

  isRuntimePathStr = v: builtins.isString v && builtins.match "/.*" v != null;
  isHexId = v: builtins.isString v && builtins.match "[0-9a-fA-F]{16}" v != null;
  stripNl = replaceStrings [ "\n" "\r" ] [ "" "" ];

  validId = v: v == null || isRuntimePathStr v || builtins.isPath v || isHexId v;

  # -> null | { static = "<16 hex>"; } | { runtime = "/path/to/secret"; }
  resolveId = v:
    if v == null then null
    else if isRuntimePathStr v then { runtime = v; }
    else if builtins.isPath v then { static = stripNl (builtins.readFile v); }
    else { static = v; };

  # profile-level deviceId wins over the global one
  profileDeviceId = p:
    let own = resolveId p.deviceId;
    in if own != null then own else resolveId cfg.deviceId;

  # wdtt://IP:DTLS_PORT:PORT2:PORT3:PASSWORD:HASH1,HASH2#Name
  parseLink = raw:
    let
      stripped = replaceStrings [ "wdtt://" "\n" "\r" ] [ "" "" "" ] raw;
      fields = splitString ":" stripped;
    in
    if length fields < 6 then
      throw "services.qwdtt: a profile `link` is not a valid wdtt:// link (expected at least 6 ':'-separated fields)"
    else
      let
        rest = concatStringsSep ":" (drop 4 fields);
        beforeName = head (splitString "#" rest);
        pwParts = splitString ":" beforeName;
        hashes = filter (h: h != "") (splitString "," (last pwParts));
        password = concatStringsSep ":" (init pwParts);
      in
      {
        peer = "${elemAt fields 0}:${elemAt fields 1}";
        inherit password hashes;
      };

  mkProfileJson = name: p:
    let
      id = profileDeviceId p;
      idAttr = optionalAttrs (id != null && id ? static) { device_id = id.static; };
      base =
        if isRuntimePathStr p.link then
          { link_file = p.link; }
        else
          parseLink (if builtins.isPath p.link then builtins.readFile p.link else p.link);
    in
    pkgs.writeText "qwdtt-ro-${name}.json" (builtins.toJSON (
      base // {
        listen = "127.0.0.1:9000";
        priority = p.priority;
      } // idAttr
    ));

  # ---------------------------------------------------------------------------
  # Secret manager detection
  # ---------------------------------------------------------------------------

  usesRuntimeSecrets =
    isRuntimePathStr cfg.deviceId
    || any (p: isRuntimePathStr p.link || isRuntimePathStr p.deviceId)
         (attrValues cfg.profiles);

  sopsActive = options ? sops && (config.sops.secrets or { }) != { };
  sopsSystemd = sopsActive && (config.sops.useSystemdActivation or false);
  agenixActive = options ? age && (config.age.secrets or { }) != { };

  detectedManager =
    if sopsActive then "sops-nix"
    else if agenixActive then "agenix"
    else "none";

  manager = if cfg.secretsManager == "auto" then detectedManager else cfg.secretsManager;

  # Only depend on activation scripts that will really exist,
  # otherwise evaluation fails with "attribute 'setupSecrets' missing".
  secretDeps =
    if manager == "sops-nix" then optional (sopsActive && !sopsSystemd) "setupSecrets"
    else if manager == "agenix" then optional agenixActive "agenixInstall"
    else [ ];

in
{
  options.services.qwdtt = {
    enable = mkEnableOption "qwdtt CLI with capabilities";

    deviceId = mkOption {
      type = types.nullOr (types.either types.str types.path);
      default = null;
      example = "0fd4ffcddb764351";
      description = ''
        Global Device ID (16 hex characters).
        Can be a string, or a path to a file containing the ID
        (e.g. a sops-nix / agenix secret path).
        If not set, will be generated automatically on first run.
      '';
    };

    package = mkOption {
      type = types.package;
      default = qwdtt-package;
      defaultText = literalExpression "qwdtt";
      description = ''
        The qwdtt package to use.
      '';
    };

    secretsManager = mkOption {
      type = types.enum [ "auto" "sops-nix" "agenix" "none" ];
      default = "auto";
      description = ''
        Secret manager that provides runtime paths used in `link` / `deviceId`.
        "auto" prefers sops-nix, then agenix.
        Use "none" if the files are provisioned by other means
        (you are then responsible for them existing at activation time).
      '';
    };

    wrappers = {
      enable = mkOption {
        type = types.bool;
        default = true;
        description = ''
          Whether to create a security wrapper with capabilities for qwdtt.
          This allows running the tools without sudo.
          Enabled by default when services.qwdtt.enable = true;.
        '';
      };

      group = mkOption {
        type = types.str;
        default = "users";
        description = ''
          Group that can execute the wrapped binaries.
        '';
      };
    };

    enableBashIntegration = mkOption {
      type = types.bool;
      default = false;
      description = ''
        Whether to enable bash completion for qwdtt.
        This will install the completion script to /etc/bash_completion.d/.
      '';
    };

    enableFishIntegration = mkOption {
      type = types.bool;
      default = false;
      description = ''
        Whether to enable fish completion for qwdtt.
        This will install the completion script to fish completions directory.
      '';
    };

    profiles = mkOption {
      type = types.attrsOf (types.submodule {
        options = {
          link = mkOption {
            type = types.either types.str types.path;
            description = ''
              wdtt:// link or path to a file containing the link.
              Example: "wdtt://1.2.3.4:56000:56001:9000:password:hash1,hash2#Name"
            '';
          };

          priority = mkOption {
            type = types.int;
            default = 0;
            description = ''
              Profile priority for auto-switch mode.
              Higher values are tried first.
            '';
          };

          deviceId = mkOption {
            type = types.nullOr (types.either types.str types.path);
            default = null;
            example = "0fd4ffcddb759420";
            description = ''
              Device ID for this specific profile (16 hex characters).
              Can be a string or a path to a file containing the ID.
              If not set, uses the global deviceId or generates automatically.
            '';
          };
        };
      });
      default = { };
      example = literalExpression ''
        {
          myserver = {
            link = "wdtt://1.2.3.4:56000:56001:9000:pass:hash#MyServer";
            priority = 100;
            deviceId = "0fd4ffcddb759420";
          };
          backup = {
            link = "/run/secrets/backup-server-link";
            priority = 50;
            deviceId = "/run/secrets/backup-device-id";
          };
        }
      '';
      description = ''
        Read-only profiles managed by NixOS configuration.
        Profile names will be prefixed with "ro-" (e.g., "myserver" becomes "ro-myserver").
        These profiles are read-only and can only be enabled/disabled via 'qwdtt enable/disable' commands.
        Use regular 'add' command to create user-managed profiles instead.
      '';
    };

    users = mkOption {
      type = types.listOf types.str;
      default = [ ];
      example = [ "alice" "bob" ];
      description = ''
        List of users for whom read-only profiles should be created.
        Profiles will be created in each user's ~/.config/qwdtt/ro-profiles/ directory.
        Profiles are always created for root.
      '';
    };
  };

  config = mkIf cfg.enable {
    assertions = [
      {
        assertion = validId cfg.deviceId;
        message = "services.qwdtt.deviceId must be 16 hex characters (e.g. '0fd4ffcddb764351') or an absolute path to a file containing it.";
      }
      {
        assertion = all (p: validId p.deviceId) (attrValues cfg.profiles);
        message = "services.qwdtt.profiles.<name>.deviceId must be 16 hex characters (e.g. '0fd4ffcddb764351') or an absolute path to a file containing it.";
      }
      {
        assertion =
          !(usesRuntimeSecrets && cfg.secretsManager == "auto" && detectedManager == "none");
        message = ''
          services.qwdtt: a `link` or `deviceId` points to a runtime file path
          (e.g. /run/secrets/...), but no secret manager was detected.

          Pick one of the following:

          # sops-nix (recommended)
          imports = [ inputs.sops-nix.nixosModules.sops ];
          sops.defaultSopsFile = ./secrets.yaml;
          sops.age.keyFile = "/var/lib/sops-nix/key.txt";
          sops.secrets."qwdtt/myserver-link" = { };
          sops.secrets."qwdtt/device-id" = { };
          services.qwdtt.deviceId = config.sops.secrets."qwdtt/device-id".path;
          services.qwdtt.profiles.myserver.link =
            config.sops.secrets."qwdtt/myserver-link".path;

          # agenix
          imports = [ inputs.agenix.nixosModules.default ];
          age.secrets.qwdtt-link.file = ./secrets/qwdtt-link.age;
          services.qwdtt.profiles.myserver.link = config.age.secrets.qwdtt-link.path;

          # Files provisioned manually / by another tool (no ordering is guaranteed)
          services.qwdtt.secretsManager = "none";
          services.qwdtt.profiles.myserver.link = "/etc/qwdtt/myserver.link";

          # Or avoid secret files: inline the wdtt:// link / 16-hex device id
          # (it will end up world-readable in the Nix store).
        '';
      }
      {
        assertion = !(manager == "sops-nix" && sopsSystemd && usesRuntimeSecrets);
        message = ''
          services.qwdtt: sops-nix is using systemd activation
          (sops.useSystemdActivation = true), so the `setupSecrets` activation
          script does not exist and secrets are not available when qwdtt's
          activation scripts run.

          Either disable sops.useSystemdActivation, or set
          services.qwdtt.secretsManager = "none" and make sure the secret
          files are installed before use.
        '';
      }
    ];

    environment.systemPackages = [
      cfg.package
      pkgs.curl
      pkgs.iputils
    ];

    # -------------------------------------------------------------------------
    # Global device id (root)
    # -------------------------------------------------------------------------
    system.activationScripts.qwdtt-device-id = mkIf (cfg.deviceId != null) {
      deps = secretDeps;
      text =
        let id = resolveId cfg.deviceId;
        in ''
          mkdir -p /root/.config/qwdtt
          chmod 700 /root/.config/qwdtt
          ${if id ? static then ''
            printf '%s' ${escapeShellArg id.static} > /root/.config/qwdtt/device_id
            chmod 600 /root/.config/qwdtt/device_id
          '' else ''
            if [ -r ${escapeShellArg id.runtime} ] && [ -s ${escapeShellArg id.runtime} ]; then
              tr -d '\n\r' < ${escapeShellArg id.runtime} > /root/.config/qwdtt/device_id
              chmod 600 /root/.config/qwdtt/device_id
            else
              echo "qwdtt: ERROR: device id file ${id.runtime} is missing or empty, device_id not written" >&2
            fi
          ''}
        '';
    };

    # -------------------------------------------------------------------------
    # Read-only profiles (root + cfg.users)
    # -------------------------------------------------------------------------
    system.activationScripts.qwdtt-ro-profiles = mkIf (cfg.profiles != { }) {
      deps = secretDeps;
      text =
        let
          keepNames = mapAttrsToList (name: _: "ro-${name}") cfg.profiles;

          profileCalls = concatStringsSep "\n" (mapAttrsToList (name: p:
            let
              id = profileDeviceId p;
              idFile = if id != null && id ? runtime then id.runtime else "";
            in ''
              install_ro_profile "$home" "$user" ${escapeShellArg "ro-${name}"} ${mkProfileJson name p} ${escapeShellArg idFile}
            ''
          ) cfg.profiles);
        in ''
          keep_profiles=${escapeShellArg (toString keepNames)}

          # Remove ro-* profiles that are no longer in the configuration
          clean_stale_ro_profiles() {
            local dir="$1" prof name keep k
            [ -d "$dir" ] || return 0
            for prof in "$dir"/ro-*.json; do
              [ -f "$prof" ] || continue
              name="$(basename "$prof" .json)"
              keep=0
              for k in $keep_profiles; do
                if [ "$name" = "$k" ]; then keep=1; break; fi
              done
              if [ "$keep" -eq 0 ]; then rm -f "$prof"; fi
            done
          }

          # install_ro_profile HOME USER NAME SRC_JSON DEVICE_ID_FILE
          # A missing/empty device id secret skips ONLY this profile (it never
          # aborts activation), so the profile can't silently get another id.
          install_ro_profile() {
            local home="$1" user="$2" name="$3" src="$4" idfile="$5"
            local cfgdir="$home/.config/qwdtt"
            local dir="$cfgdir/ro-profiles"
            local dst="$dir/$name.json"
            local id_value

            if [ ! -d "$home/.config" ]; then
              mkdir -p "$home/.config"
              chown "$user": "$home/.config"
            fi
            mkdir -p "$dir"
            chmod 700 "$cfgdir" "$dir"
            rm -f "$dst"

            if [ -n "$idfile" ]; then
              if [ -r "$idfile" ] && id_value="$(tr -d '\n\r' < "$idfile")" && [ -n "$id_value" ]; then
                ${pkgs.jq}/bin/jq --arg id "$id_value" '. + {device_id: $id}' "$src" > "$dst"
              else
                echo "qwdtt: ERROR: device id file $idfile is missing or empty, skipping profile $name for $user" >&2
                rm -f "$dst"
                return 0
              fi
            else
              cat "$src" > "$dst"
            fi

            chmod 400 "$dst"
            chown "$user": "$dst" "$dir" "$cfgdir"
          }

          for user in root ${escapeShellArgs cfg.users}; do
            home="$(${pkgs.getent}/bin/getent passwd "$user" | cut -d: -f6)"
            if [ -z "$home" ] || [ ! -d "$home" ]; then
              continue
            fi

            clean_stale_ro_profiles "$home/.config/qwdtt/ro-profiles"

            ${profileCalls}
          done
        '';
    };

    security.wrappers = mkIf cfg.wrappers.enable {
      qwdtt = {
        source = "${cfg.package}/bin/qwdtt";
        capabilities = "cap_net_admin+eip";
        owner = "root";
        group = cfg.wrappers.group;
        permissions = "u+rx,g+x";
      };
    };

    programs.bash.completion.enable = mkIf cfg.enableBashIntegration true;

    environment.pathsToLink = mkIf cfg.enableBashIntegration [
      "/share/bash-completion"
    ];

    programs.fish.enable = mkIf cfg.enableFishIntegration true;
  };
}
