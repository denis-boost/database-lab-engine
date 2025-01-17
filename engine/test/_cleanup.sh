#!/bin/bash
set -euxo pipefail

DLE_TEST_MOUNT_DIR="/var/lib/test/dblab_mount"
DLE_TEST_POOL_NAME="test_dblab_pool"
ZFS_FILE="$(pwd)/zfs_file"

# Stop and remove test Docker containers
sudo docker ps -aq --filter label="test_dblab_pool" | xargs --no-run-if-empty sudo docker rm -f
sudo docker ps -aq --filter label="dblab_test" | xargs --no-run-if-empty sudo docker rm -f

# Remove unused Docker images
sudo docker images --filter=reference='registry.gitlab.com/postgres-ai/database-lab/dblab-server:*' -q | xargs --no-run-if-empty sudo docker rmi || echo "Docker image removal finished with errors but it is OK to ignore them."

# Clean up data directory
sudo rm -rf ${DLE_TEST_MOUNT_DIR}/${DLE_TEST_POOL_NAME}/data/*

# Remove dump directory
sudo umount ${DLE_TEST_MOUNT_DIR}/${DLE_TEST_POOL_NAME}/dump \
  || echo "Unmounting dump directory finished with errors but it is OK to ignore them."
sudo rm -rf ${DLE_TEST_MOUNT_DIR}/${DLE_TEST_POOL_NAME}/dump \
  || echo "Dump directory removal finished with errors but it is OK to ignore them."

# Clean up pool directory
sudo rm -rf ${DLE_TEST_MOUNT_DIR}/${DLE_TEST_POOL_NAME}/* \
  || echo "Cleaning up pool directory finished with errors but it is OK to ignore them."

# To start from the very beginning: destroy ZFS storage pool
sudo zpool destroy test_dblab_pool \
  || echo "Destroying ZFS storage pool finished with errors but it is OK to ignore them."

# Remove ZFS FILE
sudo rm -f "${ZFS_FILE}"

# Remove CLI configuration
dblab config remove test \
  || echo "Cannot remove CLI configuration but this was optional (ignore the error)."
