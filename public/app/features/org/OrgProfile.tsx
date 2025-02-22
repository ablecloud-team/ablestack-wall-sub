import React, { FC } from 'react';
import { Input, Field, FieldSet, Button, Form } from '@grafana/ui';

export interface Props {
  orgName: string;
  onSubmit: (orgName: string) => void;
}

interface FormDTO {
  orgName: string;
}

const OrgProfile: FC<Props> = ({ onSubmit, orgName }) => {
  return (
    <Form defaultValues={{ orgName }} onSubmit={({ orgName }: FormDTO) => onSubmit(orgName)}>
      {({ register }) => (
        <FieldSet label="조직 프로필">
          <Field label="조직 이름">
            <Input id="org-name-input" type="text" {...register('orgName', { required: true })} />
          </Field>

          <Button type="submit">조직 이름 수정</Button>
        </FieldSet>
      )}
    </Form>
  );
};

export default OrgProfile;
