import React, { FC, useState } from 'react';
import { Organization } from 'app/types';
import { Button, ConfirmModal } from '@grafana/ui';

interface Props {
  orgs: Organization[];
  onDelete: (orgId: number) => void;
}

export const AdminOrgsTable: FC<Props> = ({ orgs, onDelete }) => {
  const [deleteOrg, setDeleteOrg] = useState<Organization>();
  return (
    <table className="filter-table form-inline filter-table--hover">
      <thead>
        <tr>
          <th>ID</th>
          <th>이름</th>
          <th style={{ width: '1%' }}></th>
        </tr>
      </thead>
      <tbody>
        {orgs.map((org) => (
          <tr key={`${org.id}-${org.name}`}>
            <td className="link-td">
              <a href={`admin/orgs/edit/${org.id}`}>{org.id}</a>
            </td>
            <td className="link-td">
              <a href={`admin/orgs/edit/${org.id}`}>{org.name}</a>
            </td>
            <td className="text-right">
              <Button
                variant="destructive"
                size="sm"
                icon="times"
                onClick={() => setDeleteOrg(org)}
                aria-label="Delete org"
              />
            </td>
          </tr>
        ))}
      </tbody>
      {deleteOrg && (
        <ConfirmModal
          isOpen
          icon="trash-alt"
          title="삭제"
          body={
            <div>
              {deleteOrg.name}&apos; 조직을 삭제 하시겠습니까?
              <br /> <small>이 조직의 모든 대시보드가 제거됩니다!</small>
            </div>
          }
          confirmText="삭제"
          onDismiss={() => setDeleteOrg(undefined)}
          onConfirm={() => {
            onDelete(deleteOrg.id);
            setDeleteOrg(undefined);
          }}
        />
      )}
    </table>
  );
};
