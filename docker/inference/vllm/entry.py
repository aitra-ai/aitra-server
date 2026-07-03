import os
import sys
import glob

DOWNLOAD_DIR = "/workspace"
REPO_ID = os.environ['REPO_ID']
REVISION = os.getenv('REVISION', 'main')
TOKEN = os.environ.get('ACCESS_TOKEN', '')
ENDPOINT = os.environ.get('HF_ENDPOINT', '')

download_success = False
model_dir = os.path.join(DOWNLOAD_DIR, REPO_ID)


def is_lfs_pointer(filepath):
    """Check if a file is a Git LFS pointer (small text file with 'oid sha256:')"""
    try:
        if os.path.getsize(filepath) > 1024:  # LFS pointers are tiny
            return False
        with open(filepath, 'r', errors='ignore') as f:
            content = f.read(200)
            return 'oid sha256:' in content
    except Exception:
        return False


def check_model_complete(directory):
    """Check if real model weight files (not LFS pointers) are present"""
    if not os.path.isdir(directory):
        return False
    config_file = os.path.join(directory, "config.json")
    if not os.path.exists(config_file):
        return False
    weight_files = glob.glob(os.path.join(directory, "*.safetensors")) + \
                   glob.glob(os.path.join(directory, "*.bin")) + \
                   glob.glob(os.path.join(directory, "*.gguf"))
    # Filter out LFS pointer files
    real_weights = [f for f in weight_files if not is_lfs_pointer(f)]
    if len(real_weights) == 0:
        print(f"  No real weight files found (found {len(weight_files)} LFS pointers)")
        return False
    total_size = sum(os.path.getsize(f) for f in real_weights)
    print(f"  Found {len(real_weights)} weight files, total size: {total_size / 1e9:.1f}GB")
    return True


# Step 1: Try csghub download first (original behavior)
if ENDPOINT:
    try:
        from pycsghub.snapshot_download import snapshot_download
        os.environ['CSGHUB_DOMAIN'] = ENDPOINT
        snapshot_download(REPO_ID, cache_dir=DOWNLOAD_DIR, endpoint=ENDPOINT, token=TOKEN, revision=REVISION)
        if check_model_complete(model_dir):
            print(f"Downloaded {REPO_ID} from csghub ({ENDPOINT}) - model files verified")
            download_success = True
        else:
            print(f"csghub download incomplete - model weight files missing or are LFS pointers")
    except Exception as e:
        print(f"Failed to download from csghub: {e}")

# Step 2: Fallback to hf-mirror.com
if not download_success:
    print("=" * 60)
    print("Falling back to hf-mirror.com download...")
    print("=" * 60)
    try:
        from huggingface_hub import constants as hf_constants
        from huggingface_hub import snapshot_download

        # Always use hf-mirror.com for fallback
        os.environ['HF_ENDPOINT'] = 'https://hf-mirror.com'
        os.environ.pop('HF_HUB_OFFLINE', None)
        os.environ['HF_HUB_OFFLINE'] = '0'
        # Force disable offline mode - the constant is cached at import time by pycsghub
        hf_constants.HF_HUB_OFFLINE = False
        print(f"  Offline mode disabled: HF_HUB_OFFLINE={hf_constants.HF_HUB_OFFLINE}")

        # Use HF_REPO_ID env var if available, otherwise use REPO_ID
        hf_repo_id = os.environ.get('HF_REPO_ID', REPO_ID)

        # Use 'main' revision for HF fallback - csghub REVISION is a local commit hash
        hf_revision = os.environ.get('HF_REVISION', 'main')
        print(f"Downloading {hf_repo_id} (rev={hf_revision}) from hf-mirror.com to {model_dir}...")
        path = snapshot_download(
            hf_repo_id,
            cache_dir=DOWNLOAD_DIR,
            revision=hf_revision,
            token=TOKEN if TOKEN else None,
            local_dir=model_dir,
        )
        print(f"Download complete: {path}")
        download_success = True
    except Exception as e:
        print(f"Failed to download from hf-mirror: {e}")
        import traceback
        traceback.print_exc()

if not download_success:
    print("ERROR: All download methods failed!")
    sys.exit(1)
