import { bundleManager } from '@kit.AbilityKit';

export class ClientMetadata {
  private static cachedVersion: string = '';

  static version(): string {
    if (!ClientMetadata.cachedVersion) {
      try {
        ClientMetadata.cachedVersion = bundleManager.getBundleInfoForSelfSync(
          bundleManager.BundleFlag.GET_BUNDLE_INFO_DEFAULT).versionName;
      } catch (error) { console.error(`Read app version failed: ${error.message}`); }
    }
    return ClientMetadata.cachedVersion;
  }
}
