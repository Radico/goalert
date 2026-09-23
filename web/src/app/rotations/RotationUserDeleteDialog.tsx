import React from 'react'
import { gql, useQuery, useMutation } from 'urql'
import FormDialog from '../dialogs/FormDialog'
import Spinner from '../loading/components/Spinner'
import { GenericError } from '../error-pages'

const query = gql`
  query ($id: ID!) {
    rotation(id: $id) {
      id
      userIDs
      activeUserIndex
      users {
        id
        name
      }
    }
  }
`

const mutation = gql`
  mutation ($input: UpdateRotationInput!) {
    updateRotation(input: $input)
  }
`
const RotationUserDeleteDialog = (props: {
  rotationID: string
  userIndex: number
  onClose: () => void
}): React.JSX.Element => {
  const { rotationID, userIndex, onClose } = props
  const [deleteUserMutationStatus, deleteUserMutation] = useMutation(mutation)
  const [{ fetching, data, error }] = useQuery({
    query,
    variables: {
      id: rotationID,
    },
  })

  if (fetching && !data) return <Spinner />
  if (error) return <GenericError error={error.message} />

  const { userIDs, users } = data.rotation

  return (
    <FormDialog
      title='Are you sure?'
      confirm
      subTitle={`This will delete ${
        users[userIndex] ? users[userIndex].name : null
      } from this rotation.`}
      onClose={onClose}
      errors={
        deleteUserMutationStatus.error ? [deleteUserMutationStatus.error] : []
      }
      onSubmit={() => {
        const remaining = userIDs.filter(
          (_: string, index: number) => index !== userIndex,
        )

        // Removing someone ahead of the active user shifts them down one.
        // Removing the active user leaves the index pointing at whoever moved
        // up into the slot, except at the end of the list, where it has to wrap
        // -- otherwise this sends an index the rotation no longer has and the
        // server rejects it with "invalid index for rotation".
        const shifted =
          userIndex < data.rotation.activeUserIndex
            ? data.rotation.activeUserIndex - 1
            : data.rotation.activeUserIndex
        const activeUserIndex = shifted >= remaining.length ? 0 : shifted

        return deleteUserMutation(
          {
            input: {
              id: rotationID,
              userIDs: remaining,
              activeUserIndex,
            },
          },
          { additionalTypenames: ['Rotation'] },
        ).then((res) => {
          if (res.error) return
          onClose()
        })
      }}
    />
  )
}

export default RotationUserDeleteDialog
