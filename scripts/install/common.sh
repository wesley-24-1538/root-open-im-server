#!/usr/bin/env bash
# Copyright © 2023 OpenIM. All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


# Common utilities, variables and checks for all build scripts.
set -o errexit
set +o nounset
set -o pipefail


openim::common::setSpecificServerVariables(){

  echo "wesley input all parameters: $*" ## 形如 rpc api push

  if [[ "" = "$*" ]];then
    return
  fi

  ## 避免多次调用
  if [[ ${#SERVER_TARGETS_FROM_CUSTOMER[@]} -ne 0 ]];then
     	return
  fi

  SERVER_TARGETS_FROM_CUSTOMER=("$@")
  EXECUTE_SCRIPTS_FROM_CUSTOMER=("openim-msggateway.sh" "openim-tools.sh")

  for item in $SERVER_TARGETS_FROM_CUSTOMER
  do
    case $item in
      *"api"*)
        EXECUTE_SCRIPTS_FROM_CUSTOMER+=("openim-api.sh")
        ;;
      *"rpc"*)
        EXECUTE_SCRIPTS_FROM_CUSTOMER+=("openim-rpc.sh")
        ;;
      *"push"*)
        EXECUTE_SCRIPTS_FROM_CUSTOMER+=("openim-push.sh")
        ;;
      *"msgtransfer"*)
        EXECUTE_SCRIPTS_FROM_CUSTOMER+=("openim-msgtransfer.sh")
        ;;
      *"crontask"*)
        EXECUTE_SCRIPTS_FROM_CUSTOMER+=("openim-crontask.sh")
        ;;
      *)
        ##do nothing
      esac
  done
echo "===wesley==>SERVER_TARGETS_FROM_CUSTOMER: ${SERVER_TARGETS_FROM_CUSTOMER[@]} ||| EXECUTE_SCRIPTS_FROM_CUSTOMER:${EXECUTE_SCRIPTS_FROM_CUSTOMER[@]}"

}

SERVER_TARGETS_FROM_CUSTOMER=()
EXECUTE_SCRIPTS_FROM_CUSTOMER=()
##第一时间给参数赋值
openim::common::setSpecificServerVariables "$@"

# Sourced flag
COMMON_SOURCED=true
# The root of the build/dist directory
OPENIM_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")"/../.. && pwd -P)
source "${OPENIM_ROOT}/scripts/lib/init.sh"
# Make sure the environment is only called via common to avoid too much nesting
source "${OPENIM_ROOT}/scripts/install/environment.sh"

# This function returns a list of Prometheus ports for various services
# based on the provided configuration. Each service has its own dedicated
# port for monitoring purposes.
openim::common::prometheus_port() {
  # Declare an array to hold all the Prometheus ports for different services
  local targets=(
    ${USER_PROM_PORT}               # Prometheus port for user service
    ${FRIEND_PROM_PORT}             # Prometheus port for friend service
    ${MESSAGE_PROM_PORT}            # Prometheus port for message service
    ${MSG_GATEWAY_PROM_PORT}        # Prometheus port for message gateway service
    ${GROUP_PROM_PORT}              # Prometheus port for group service
    ${AUTH_PROM_PORT}               # Prometheus port for authentication service
    ${PUSH_PROM_PORT}               # Prometheus port for push notification service
    ${CONVERSATION_PROM_PORT}       # Prometheus port for conversation service
    ${RTC_PROM_PORT}                # Prometheus port for real-time communication service
    ${THIRD_PROM_PORT}              # Prometheus port for third-party integrations service
    ${MSG_TRANSFER_PROM_PORT}       # Prometheus port for message transfer service
  )
  # Print the list of ports
  echo "${targets[@]}"
}
IFS=" " read -ra OPENIM_PROM_PORT_TARGETS <<< "$(openim::common::prometheus_port)"
readonly OPENIM_PROM_PORT_TARGETS
readonly OPENIM_PROM_PORT_LISTARIES=("${OPENIM_PROM_PORT_TARGETS[@]##*/}")

openim::common::service_name() {
    local targets=(
        openim-user
        openim-friend
        openim-msg
        openim-msg-gateway
        openim-group
        openim-auth
        openim-push
        openim-conversation
        openim-third
        # openim-msg-transfer

        # api
        openim-api
        openim-ws
    )
    echo "${targets[@]}"
}

IFS=" " read -ra OPENIM_SERVER_NAME_TARGETS <<< "$(openim::common::service_name)"
readonly OPENIM_SERVER_NAME_TARGETS

# Storing all the defined ports in an array for easy management and access.
# This array consolidates the port numbers for all the services defined above.
openim::common::service_port() {
  local targets=(
    ${OPENIM_USER_PORT}            # User service
    ${OPENIM_FRIEND_PORT}          # Friend service
    ${OPENIM_MESSAGE_PORT}         # Message service
    ${OPENIM_MESSAGE_GATEWAY_PORT} # Message gateway
    ${OPENIM_GROUP_PORT}           # Group service
    ${OPENIM_AUTH_PORT}            # Authorization service
    ${OPENIM_PUSH_PORT}            # Push service
    ${OPENIM_CONVERSATION_PORT}    # Conversation service
    ${OPENIM_THIRD_PORT}           # Third-party service

    # API PORT
    ${API_OPENIM_PORT}             # API service
    ${OPENIM_WS_PORT}              # WebSocket service
  )
    ##wesley 停用指定的服务端口
  echo "${targets[@]}"
}
IFS=" " read -ra OPENIM_SERVER_PORT_TARGETS <<< "$(openim::common::service_port)"
readonly OPENIM_SERVER_PORT_TARGETS
readonly OPENIM_SERVER_PORT_LISTARIES=("${OPENIM_SERVER_PORT_TARGETS[@]##*/}")

openim::common::dependency_name() {
    local targets=(
        #redis
        #zookeeper
        #kafka
        #mongodb
        #minio
    )
    echo "${targets[@]}"
}

IFS=" " read -ra OPENIM_DEPENDENCY_TARGETS <<< "$(openim::common::dependency_name)"
readonly OPENIM_DEPENDENCY_TARGETS

# This function returns a list of ports for various services
#  - zookeeper
#  - kafka
#  - mongodb
#  - redis
#  - minio
openim::common::dependency_port() {
  local targets=(
    #${REDIS_PORT} # Redis port
    #${ZOOKEEPER_PORT} # Zookeeper port
    #${KAFKA_PORT} # Kafka port
    #${MONGO_PORT} # MongoDB port
    #${MINIO_PORT} # MinIO port
  )
    echo "${targets[@]}"
}
IFS=" " read -ra OPENIM_DEPENDENCY_PORT_TARGETS <<< "$(openim::common::dependency_port)"
readonly OPENIM_DEPENDENCY_PORT_TARGETS
readonly OPENIM_DEPENDENCY_PORT_LISTARIES=("${OPENIM_DEPENDENCY_PORT_TARGETS[@]##*/}")

# Execute commands that require root permission without entering a password
function openim::common::sudo {
  echo ${LINUX_PASSWORD} | sudo -S $1
}
